import AppKit

/// The routing policies: pick one, or bring your own from a rules file.
final class RoutingViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate {
    private let table = NSTableView()
    private var rows: [String] = []
    private let builtin: Set<String> = ["all", "lan-direct"]

    override func loadView() {
        let root = NSView()

        let add = NSButton(image: NSImage(systemSymbolName: "plus", accessibilityDescription: L("Import a policy")) ?? NSImage(), target: self, action: #selector(importPolicy(_:)))
        add.bezelStyle = .rounded
        add.toolTip = L("Import a policy from a rules file")
        let hint = NSTextField(labelWithString: L("A policy decides which connections go through the server."))
        hint.font = .systemFont(ofSize: 12)
        hint.textColor = .secondaryLabelColor
        hint.lineBreakMode = .byTruncatingTail
        let bar = NSStackView(views: [add, hint, NSView()])
        bar.orientation = .horizontal
        bar.spacing = 10
        bar.edgeInsets = NSEdgeInsets(top: 10, left: 14, bottom: 8, right: 14)

        for (id, title, width) in [("mark", "", 22.0), ("name", L("Policy"), 300.0), ("kind", L("Kind"), 140.0)] {
            let column = NSTableColumn(identifier: .init(id))
            column.title = title
            column.width = width
            if id == "mark" { column.maxWidth = 22; column.minWidth = 22 }
            table.addTableColumn(column)
        }
        table.style = .inset
        table.rowSizeStyle = .medium
        table.allowsMultipleSelection = false
        table.columnAutoresizingStyle = .firstColumnOnlyAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(useSelected(_:))
        table.setAccessibilityLabel(L("Routing"))
        let menu = NSMenu()
        menu.addItem(withTitle: L("Use This Policy"), action: #selector(useSelected(_:)), keyEquivalent: "").target = self
        table.menu = menu

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false

        for subview in [bar, scroll] as [NSView] {
            subview.translatesAutoresizingMaskIntoConstraints = false
            root.addSubview(subview)
        }
        NSLayoutConstraint.activate([
            bar.topAnchor.constraint(equalTo: root.topAnchor),
            bar.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            bar.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            scroll.topAnchor.constraint(equalTo: bar.bottomAnchor),
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

    @objc private func reload() {
        rows = Model.shared.view.policies
        table.reloadData()
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard let column = tableColumn else { return nil }
        let name = rows[row]
        let chosen = name == Model.shared.view.policy
        let cell = NSTableCellView()
        if column.identifier.rawValue == "mark" {
            let image = NSImageView()
            if chosen {
                image.image = NSImage(systemSymbolName: "checkmark", accessibilityDescription: L("Selected"))
                image.contentTintColor = .controlAccentColor
            }
            image.translatesAutoresizingMaskIntoConstraints = false
            cell.addSubview(image)
            NSLayoutConstraint.activate([
                image.centerXAnchor.constraint(equalTo: cell.centerXAnchor),
                image.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
            ])
            return cell
        }
        let text = column.identifier.rawValue == "kind"
            ? (builtin.contains(name) ? L("Built-in") : L("Imported"))
            : policyTitle(name)
        let label = NSTextField(labelWithString: text)
        label.lineBreakMode = .byTruncatingTail
        if column.identifier.rawValue == "kind" { label.textColor = .secondaryLabelColor }
        if chosen, column.identifier.rawValue == "name" {
            label.font = .systemFont(ofSize: NSFont.systemFontSize, weight: .semibold)
        }
        label.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(label)
        cell.textField = label
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 2),
            label.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -2),
            label.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    @objc private func useSelected(_ sender: Any?) {
        let row = table.clickedRow >= 0 ? table.clickedRow : table.selectedRow
        if row >= 0, row < rows.count { Model.shared.select(policy: rows[row]) }
    }

    @objc private func importPolicy(_ sender: Any?) {
        guard let window = view.window else { return }
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.plainText, .text]
        panel.allowsMultipleSelection = false
        panel.message = L("Choose a rules file: one rule per line, ending with MATCH.")
        panel.beginSheetModal(for: window) { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            guard let text = try? String(contentsOf: url, encoding: .utf8) else { return }
            // The file name becomes the policy name, in the form the engine takes.
            let raw = url.deletingPathExtension().lastPathComponent.lowercased()
            let name = String(raw.map { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") ? $0 : "-" })
            Model.shared.importPolicy(name: name, text: text) { error in
                guard let error, let window = self?.view.window else { return }
                let alert = NSAlert()
                alert.messageText = L("The policy was not imported")
                alert.informativeText = error
                alert.beginSheetModal(for: window)
            }
        }
    }
}
