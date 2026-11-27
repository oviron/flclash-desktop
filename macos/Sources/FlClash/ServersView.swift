import AppKit
import UniformTypeIdentifiers

/// The servers: one table, filled by importing, pasting or dropping links.
final class ServersViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate, NSMenuItemValidation {
    private let table = ListTableView()
    private let empty = EmptyStateView(symbol: "server.rack")
    private var rows: [Server] = []

    /// What the search field of the toolbar holds.
    var query = "" {
        didSet { if isViewLoaded, query != oldValue { reload() } }
    }

    override func loadView() {
        let root = DropView()
        root.onDrop = { [weak self] text, _ in self?.importText(text) }

        for (id, title, width) in [("mark", "", 22.0), ("name", L("Name"), 220.0), ("protocol", L("Protocol"), 190.0), ("delay", L("Latency"), 90.0), ("group", L("Group"), 100.0)] {
            let column = NSTableColumn(identifier: .init(id))
            column.title = title
            column.width = width
            column.minWidth = id == "mark" ? 22 : 60
            if id == "mark" { column.maxWidth = 22 } else {
                column.sortDescriptorPrototype = NSSortDescriptor(key: id, ascending: true)
            }
            table.addTableColumn(column)
        }
        table.style = .inset
        table.rowSizeStyle = .medium
        table.usesAlternatingRowBackgroundColors = false
        table.allowsMultipleSelection = true
        // Every column gives way together, so none is pushed out of a narrow window.
        table.columnAutoresizingStyle = .uniformColumnAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(useSelected(_:))
        table.onDelete = { [weak self] in self?.delete(nil) }
        table.setAccessibilityLabel(L("Servers"))
        let menu = NSMenu()
        menu.addItem(withTitle: L("Use This Server"), action: #selector(useSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(withTitle: L("Test Latency"), action: #selector(measureSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Delete"), action: #selector(delete(_:)), keyEquivalent: "").target = self
        table.menu = menu

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.borderType = .noBorder

        empty.button.title = L("Import from Clipboard")
        empty.button.target = self
        empty.button.action = #selector(importFromClipboardPressed(_:))

        for subview in [scroll, empty] as [NSView] {
            subview.translatesAutoresizingMaskIntoConstraints = false
            root.addSubview(subview)
        }
        NSLayoutConstraint.activate([
            scroll.topAnchor.constraint(equalTo: root.topAnchor),
            scroll.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            scroll.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            scroll.bottomAnchor.constraint(equalTo: root.bottomAnchor),
            empty.centerXAnchor.constraint(equalTo: root.centerXAnchor),
            empty.centerYAnchor.constraint(equalTo: root.centerYAnchor),
            empty.widthAnchor.constraint(lessThanOrEqualTo: root.widthAnchor, constant: -60),
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
        let chosen = Set(table.selectedRowIndexes.filter { $0 < rows.count }.map { rows[$0].id })
        let all = Model.shared.view.servers
        let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
        var list = all
        if !needle.isEmpty {
            list = list.filter { ($0.name + " " + $0.proto + " " + $0.group).lowercased().contains(needle) }
        }
        if let sort = table.sortDescriptors.first, let key = sort.key {
            if key == "delay" {
                // Measured ones by the number; no answer after them; not measured last.
                let value: (Server) -> Int = {
                    switch Model.shared.delays[$0.id] {
                    case .ms(let ms): return ms
                    case .failed: return Int.max - 1
                    default: return Int.max
                    }
                }
                list.sort { sort.ascending ? value($0) < value($1) : value($0) > value($1) }
            } else {
                let value: (Server) -> String = { key == "protocol" ? $0.proto : key == "group" ? $0.group : $0.name }
                list.sort { a, b in
                    let order = value(a).localizedStandardCompare(value(b))
                    return sort.ascending ? order == .orderedAscending : order == .orderedDescending
                }
            }
        }
        rows = list

        // One group needs no column to say so.
        let grouped = Set(all.map(\.group)).count > 1
        if let column = table.tableColumn(withIdentifier: .init("group")), column.isHidden == grouped {
            column.isHidden = !grouped
            table.sizeToFit()
        }
        table.reloadData()
        table.selectRowIndexes(IndexSet(rows.indices.filter { chosen.contains(rows[$0].id) }), byExtendingSelection: false)

        // An empty table would still draw its header line.
        table.enclosingScrollView?.isHidden = rows.isEmpty
        if all.isEmpty {
            empty.show(title: L("No servers yet"),
                       text: L("Copy a server link in Throne and paste it here, or drop a file with links onto this window."),
                       button: true)
        } else if rows.isEmpty {
            empty.show(title: L("Nothing matches the search"), text: "", button: false)
        } else {
            empty.isHidden = true
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard let column = tableColumn else { return nil }
        let server = rows[row]
        let selected = server.id == Model.shared.view.selected
        if column.identifier.rawValue == "mark" { return markCell(selected) }
        if column.identifier.rawValue == "delay" { return delayCell(Model.shared.delays[server.id]) }
        let text: String
        switch column.identifier.rawValue {
        case "protocol": text = server.proto
        case "group": text = server.group
        default: text = server.name
        }
        return textCell(text, secondary: column.identifier.rawValue != "name", strong: selected)
    }

    func tableView(_ tableView: NSTableView, sortDescriptorsDidChange oldDescriptors: [NSSortDescriptor]) { reload() }

    /// The rows an action is about: the one under a right-click when it is
    /// outside the selection, else the selection.
    private var actionRows: [Server] {
        let clicked = table.clickedRow
        let indexes = clicked >= 0 && !table.selectedRowIndexes.contains(clicked)
            ? IndexSet(integer: clicked) : table.selectedRowIndexes
        return indexes.filter { $0 < rows.count }.map { rows[$0] }
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        switch menuItem.action {
        case #selector(useSelected(_:)): return actionRows.count == 1
        case #selector(delete(_:)), #selector(measureSelected(_:)): return !actionRows.isEmpty
        default: return true
        }
    }

    @objc func useSelected(_ sender: Any?) {
        let servers = actionRows
        if servers.count == 1 { Model.shared.select(server: servers[0].id) }
    }

    @objc private func measureSelected(_ sender: Any?) {
        Model.shared.measure(servers: actionRows.map(\.id))
    }

    /// Measures every server the table shows, in its order.
    func measureAll() {
        Model.shared.measure(servers: rows.map(\.id))
    }

    /// Edit > Delete and the Delete key.
    @objc func delete(_ sender: Any?) {
        let servers = actionRows
        guard !servers.isEmpty, let window = view.window else { return }
        let alert = NSAlert()
        alert.messageText = servers.count == 1
            ? String(format: L("Delete the server “%@”?"), servers[0].name)
            : L("Delete the selected servers?")
        alert.informativeText = servers.count == 1
            ? L("To get it back, you will have to import it again.")
            : servers.map(\.name).joined(separator: ", ")
        alert.addButton(withTitle: L("Delete")).hasDestructiveAction = true
        alert.addButton(withTitle: L("Cancel"))
        alert.beginSheetModal(for: window) { response in
            if response == .alertFirstButtonReturn { Model.shared.remove(servers: servers.map(\.id)) }
        }
    }

    @objc private func importFromClipboardPressed(_ sender: Any?) { importFromClipboard() }

    func importFromClipboard() {
        guard let text = NSPasteboard.general.string(forType: .string), !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            report(title: L("The clipboard has no text"), text: L("Copy a server link first: in Throne, right-click a profile and copy it as a link."))
            return
        }
        importText(text)
    }

    func importFromFile() {
        guard let window = view.window else { return }
        let panel = NSOpenPanel()
        panel.allowedContentTypes = [.plainText, .json, .text]
        panel.allowsMultipleSelection = false
        panel.beginSheetModal(for: window) { [weak self] response in
            guard response == .OK, let url = panel.url else { return }
            guard let text = try? String(contentsOf: url, encoding: .utf8) else {
                self?.report(title: L("The file could not be read"), text: url.lastPathComponent)
                return
            }
            self?.importText(text)
        }
    }

    private func importText(_ text: String) {
        Model.shared.importServers(text) { [weak self] result, error in
            guard let self else { return }
            let imported = result?.imported.count ?? 0
            let failed = result?.failed ?? []
            // A clean import needs no dialog: the new rows are the answer.
            if failed.isEmpty, error == nil { return }
            var lines = failed.map { String(format: L("Entry %d: %@"), $0.entry, $0.reason) }
            if failed.isEmpty, let error { lines = [error] }
            let title = imported > 0
                ? String(format: L("Imported %d, not imported %d"), imported, failed.count)
                : L("Nothing was imported")
            self.report(title: title, text: lines.joined(separator: "\n\n"))
        }
    }

    private func report(title: String, text: String) {
        guard let window = view.window else { return }
        let alert = NSAlert()
        alert.messageText = title
        alert.informativeText = text
        alert.beginSheetModal(for: window)
    }
}

/// The cell of the column that marks the chosen row.
func markCell(_ chosen: Bool) -> NSView {
    let cell = NSTableCellView()
    let image = NSImageView()
    if chosen {
        image.image = NSImage(systemSymbolName: "checkmark", accessibilityDescription: L("Selected"))
        image.symbolConfiguration = .init(pointSize: 12, weight: .semibold)
        image.contentTintColor = .controlAccentColor
    }
    image.translatesAutoresizingMaskIntoConstraints = false
    cell.addSubview(image)
    cell.imageView = image
    NSLayoutConstraint.activate([
        image.centerXAnchor.constraint(equalTo: cell.centerXAnchor),
        image.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
    ])
    return cell
}

/// The cell of the latency column: a number, a spinner while it is on its way,
/// or the word that there was no answer, with the reason on hover.
func delayCell(_ delay: Model.Delay?) -> NSView {
    switch delay {
    case .ms(let ms):
        let cell = textCell(String(format: L("%d ms"), ms), secondary: true, strong: false)
        (cell as? NSTableCellView)?.textField?.font = .monospacedDigitSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)
        return cell
    case .failed(let reason):
        let cell = textCell(L("No answer"), secondary: true, strong: false)
        cell.toolTip = reason
        return cell
    case .waiting:
        let cell = NSTableCellView()
        let spinner = NSProgressIndicator()
        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(spinner)
        NSLayoutConstraint.activate([
            spinner.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 2),
            spinner.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        spinner.startAnimation(nil)
        return cell
    case nil:
        return NSTableCellView()
    }
}

/// A one-line text cell; the table recolours it on a selected row.
func textCell(_ text: String, secondary: Bool, strong: Bool) -> NSView {
    let cell = NSTableCellView()
    let label = NSTextField(labelWithString: text)
    label.lineBreakMode = .byTruncatingTail
    if secondary { label.textColor = .secondaryLabelColor }
    if strong, !secondary { label.font = .systemFont(ofSize: NSFont.systemFontSize, weight: .semibold) }
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

/// What a page shows in place of an empty list.
final class EmptyStateView: NSStackView {
    let button = NSButton(title: "", target: nil, action: nil)
    private let title = NSTextField(labelWithString: "")
    private let text = NSTextField(wrappingLabelWithString: "")

    init(symbol: String) {
        super.init(frame: .zero)
        let image = NSImageView(image: NSImage(systemSymbolName: symbol, accessibilityDescription: nil) ?? NSImage())
        image.symbolConfiguration = .init(pointSize: 34, weight: .regular)
        image.contentTintColor = .tertiaryLabelColor
        title.font = .systemFont(ofSize: 15, weight: .semibold)
        title.textColor = .secondaryLabelColor
        text.font = .systemFont(ofSize: 12)
        text.textColor = .secondaryLabelColor
        text.alignment = .center
        button.bezelStyle = .push
        orientation = .vertical
        alignment = .centerX
        spacing = 8
        setViews([image, title, text, button], in: .center)
        setCustomSpacing(12, after: image)
        setCustomSpacing(14, after: text)
        text.widthAnchor.constraint(lessThanOrEqualToConstant: 340).isActive = true
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    func show(title: String, text: String, button: Bool) {
        self.title.stringValue = title
        self.text.stringValue = text
        self.text.isHidden = text.isEmpty
        self.button.isHidden = !button
        isHidden = false
    }
}
