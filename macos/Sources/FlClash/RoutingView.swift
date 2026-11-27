import AppKit

/// The routing policies: pick one, or bring your own from a rules file.
final class RoutingViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate, NSMenuItemValidation {
    private let table = ListTableView()
    private var rows: [Policy] = []

    override func loadView() {
        let root = DropView()
        root.onDrop = { [weak self] text, url in
            self?.importPolicy(text: text, suggestedName: url?.deletingPathExtension().lastPathComponent ?? "policy")
        }

        table.addTableColumn(NSTableColumn(identifier: .init("name")))
        // A short list of things to choose from, not a sheet of data: no header.
        table.headerView = nil
        table.style = .inset
        table.rowHeight = 52
        table.allowsMultipleSelection = false
        table.columnAutoresizingStyle = .lastColumnOnlyAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(useSelected(_:))
        table.onDelete = { [weak self] in self?.delete(nil) }
        table.setAccessibilityLabel(L("Routing"))
        let menu = NSMenu()
        menu.addItem(withTitle: L("Use This Policy"), action: #selector(useSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Delete"), action: #selector(delete(_:)), keyEquivalent: "").target = self
        table.menu = menu

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.borderType = .noBorder
        scroll.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(scroll)
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: root.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: root.bottomAnchor),
        ])
        view = root
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        NotificationCenter.default.addObserver(self, selector: #selector(reload), name: .modelChanged, object: nil)
        reload()
    }

    override func viewDidLayout() {
        super.viewDidLayout()
        table.sizeToFit()
    }

    @objc private func reload() {
        let chosen = table.selectedRow >= 0 && table.selectedRow < rows.count ? rows[table.selectedRow].name : nil
        rows = Model.shared.view.policies
        table.reloadData()
        if let chosen, let index = rows.firstIndex(where: { $0.name == chosen }) {
            table.selectRowIndexes([index], byExtendingSelection: false)
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let policy = rows[row]
        let chosen = policy.name == Model.shared.view.policy

        let tile = IconTile(symbol: policySymbol(policy), tint: policyTint(policy))
        let title = NSTextField(labelWithString: policyTitle(policy.name))
        title.lineBreakMode = .byTruncatingTail
        if chosen { title.font = .systemFont(ofSize: NSFont.systemFontSize, weight: .semibold) }
        let about = NSTextField(labelWithString: policyAbout(policy))
        about.font = .systemFont(ofSize: 11)
        about.textColor = .secondaryLabelColor
        about.lineBreakMode = .byTruncatingTail
        let lines = NSStackView(views: [title, about])
        lines.orientation = .vertical
        lines.alignment = .leading
        lines.spacing = 2
        for label in [title, about] { label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal) }

        let check = NSImageView()
        if chosen {
            check.image = NSImage(systemSymbolName: "checkmark.circle.fill", accessibilityDescription: L("Selected"))
            check.symbolConfiguration = .init(pointSize: 16, weight: .medium)
            check.contentTintColor = .controlAccentColor
        }
        check.setContentHuggingPriority(.required, for: .horizontal)
        let spacer = NSView()
        spacer.setContentHuggingPriority(.init(1), for: .horizontal)
        let line = NSStackView(views: [tile, lines, spacer, check])
        line.orientation = .horizontal
        line.alignment = .centerY
        line.spacing = 12
        line.translatesAutoresizingMaskIntoConstraints = false
        let cell = NSTableCellView()
        cell.addSubview(line)
        cell.textField = title
        NSLayoutConstraint.activate([
            line.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 6),
            line.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -10),
            line.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    private var actionRow: Policy? {
        let row = table.clickedRow >= 0 ? table.clickedRow : table.selectedRow
        return row >= 0 && row < rows.count ? rows[row] : nil
    }

    /// An imported policy that is not the one in use can go.
    private func removable(_ policy: Policy?) -> Bool {
        guard let policy else { return false }
        return !policy.builtin && policy.name != Model.shared.view.policy
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        switch menuItem.action {
        case #selector(useSelected(_:)): return actionRow != nil
        case #selector(delete(_:)): return removable(actionRow)
        default: return true
        }
    }

    @objc private func useSelected(_ sender: Any?) {
        if let policy = actionRow { Model.shared.select(policy: policy.name) }
    }

    /// Edit > Delete and the Delete key.
    @objc func delete(_ sender: Any?) {
        guard let policy = actionRow, removable(policy), let window = view.window else {
            NSSound.beep()
            return
        }
        let alert = NSAlert()
        alert.messageText = String(format: L("Delete the policy “%@”?"), policy.name)
        alert.informativeText = L("To get it back, you will have to import it again.")
        alert.addButton(withTitle: L("Delete")).hasDestructiveAction = true
        alert.addButton(withTitle: L("Cancel"))
        alert.beginSheetModal(for: window) { response in
            if response == .alertFirstButtonReturn { Model.shared.remove(policy: policy.name) }
        }
    }

    func importPolicy() {
        guard let window = view.window else { return }
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.plainText, .text]
        panel.allowsMultipleSelection = false
        panel.message = L("Choose a rules file: one rule per line, ending with MATCH.")
        panel.beginSheetModal(for: window) { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            guard let text = try? String(contentsOf: url, encoding: .utf8) else { return }
            self?.importPolicy(text: text, suggestedName: url.deletingPathExtension().lastPathComponent)
        }
    }

    private func importPolicy(text: String, suggestedName: String) {
        // The file name becomes the policy name, in the form the engine takes.
        let name = String(suggestedName.lowercased().map { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") ? $0 : "-" })
        Model.shared.importPolicy(name: name, text: text) { [weak self] error in
            guard let error, let window = self?.view.window else { return }
            let alert = NSAlert()
            alert.messageText = L("The policy was not imported")
            alert.informativeText = error
            alert.beginSheetModal(for: window)
        }
    }
}

/// One line on what a policy does.
func policyAbout(_ policy: Policy) -> String {
    switch policy.name {
    case "all": return L("All traffic goes through the server.")
    case "lan-direct": return L("The home network and local addresses go directly, the rest through the server.")
    case "ru-direct": return L("Russian sites go directly, the rest through the server.")
    default: return String.localizedStringWithFormat(L("Imported, %d rules"), policy.rules)
    }
}

/// The picture of a policy.
func policySymbol(_ policy: Policy) -> String {
    switch policy.name {
    case "all": return "globe"
    case "lan-direct": return "house.fill"
    case "ru-direct": return "arrow.triangle.branch"
    default: return "doc.text.fill"
    }
}

func policyTint(_ policy: Policy) -> NSColor {
    switch policy.name {
    case "all": return .systemBlue
    case "lan-direct": return .systemTeal
    case "ru-direct": return .systemIndigo
    default: return .systemGray
    }
}

/// A symbol on a rounded square of its colour, the way System Settings draws
/// the things one chooses between.
final class IconTile: NSView {
    private let tint: NSColor

    init(symbol: String, tint: NSColor, side: CGFloat = 30) {
        self.tint = tint
        super.init(frame: .zero)
        let image = NSImageView(image: NSImage(systemSymbolName: symbol, accessibilityDescription: nil) ?? NSImage())
        image.symbolConfiguration = .init(pointSize: side * 0.47, weight: .semibold)
        image.contentTintColor = .white
        image.translatesAutoresizingMaskIntoConstraints = false
        addSubview(image)
        NSLayoutConstraint.activate([
            widthAnchor.constraint(equalToConstant: side),
            heightAnchor.constraint(equalToConstant: side),
            image.centerXAnchor.constraint(equalTo: centerXAnchor),
            image.centerYAnchor.constraint(equalTo: centerYAnchor),
        ])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    override func draw(_ dirtyRect: NSRect) {
        let shape = NSBezierPath(roundedRect: bounds, xRadius: bounds.width * 0.26, yRadius: bounds.width * 0.26)
        NSGradient(starting: tint.blended(withFraction: 0.18, of: .white) ?? tint, ending: tint)?.draw(in: shape, angle: -90)
    }
}
