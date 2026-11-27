import AppKit
import UniformTypeIdentifiers

/// The servers: one table, a search field, and the ways to add to it.
final class ServersViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate, NSSearchFieldDelegate, NSMenuItemValidation {
    private let table = NSTableView()
    private let search = NSSearchField()
    private let empty = NSTextField(labelWithString: "")
    private var rows: [Server] = []

    override func loadView() {
        let root = NSView()

        search.placeholderString = L("Search")
        search.delegate = self
        search.sendsSearchStringImmediately = true
        search.controlSize = .regular

        let add = NSPopUpButton(frame: .zero, pullsDown: true)
        add.bezelStyle = .rounded
        (add.cell as? NSPopUpButtonCell)?.arrowPosition = .arrowAtBottom
        add.addItem(withTitle: "")
        add.item(at: 0)?.image = NSImage(systemSymbolName: "plus", accessibilityDescription: L("Add servers"))
        add.addItem(withTitle: L("Import from Clipboard"))
        add.lastItem?.action = #selector(importFromClipboard(_:))
        add.lastItem?.target = self
        add.addItem(withTitle: L("Import from File…"))
        add.lastItem?.action = #selector(importFromFile(_:))
        add.lastItem?.target = self
        add.toolTip = L("Add servers")

        let bar = NSStackView(views: [add, NSView(), search])
        bar.orientation = .horizontal
        bar.spacing = 8
        bar.edgeInsets = NSEdgeInsets(top: 10, left: 14, bottom: 8, right: 14)
        search.widthAnchor.constraint(equalToConstant: 200).isActive = true

        for (id, title, width) in [("mark", "", 22.0), ("name", L("Name"), 220.0), ("protocol", L("Protocol"), 190.0), ("group", L("Group"), 120.0)] {
            let column = NSTableColumn(identifier: .init(id))
            column.title = title
            column.width = width
            column.minWidth = id == "mark" ? 22 : 80
            if id == "mark" { column.maxWidth = 22 } else {
                column.sortDescriptorPrototype = NSSortDescriptor(key: id, ascending: true)
            }
            table.addTableColumn(column)
        }
        table.style = .inset
        table.rowSizeStyle = .medium
        table.usesAlternatingRowBackgroundColors = false
        table.allowsMultipleSelection = false
        table.columnAutoresizingStyle = .firstColumnOnlyAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(useSelected(_:))
        table.setAccessibilityLabel(L("Servers"))
        let menu = NSMenu()
        menu.addItem(withTitle: L("Use This Server"), action: #selector(useSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Remove"), action: #selector(removeSelected(_:)), keyEquivalent: "").target = self
        table.menu = menu

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.borderType = .noBorder

        empty.font = .systemFont(ofSize: 13)
        empty.textColor = .secondaryLabelColor
        empty.alignment = .center

        for subview in [bar, scroll, empty] as [NSView] {
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
            empty.centerXAnchor.constraint(equalTo: scroll.centerXAnchor),
            empty.centerYAnchor.constraint(equalTo: scroll.centerYAnchor),
            empty.widthAnchor.constraint(lessThanOrEqualTo: scroll.widthAnchor, constant: -40),
        ])
        view = root
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        NotificationCenter.default.addObserver(self, selector: #selector(reload), name: .modelChanged, object: nil)
        reload()
    }

    @objc private func reload() {
        let chosen = table.selectedRow >= 0 && table.selectedRow < rows.count ? rows[table.selectedRow].id : nil
        let query = search.stringValue.trimmingCharacters(in: .whitespaces).lowercased()
        var list = Model.shared.view.servers
        if !query.isEmpty {
            list = list.filter { ($0.name + " " + $0.proto + " " + $0.group).lowercased().contains(query) }
        }
        if let sort = table.sortDescriptors.first, let key = sort.key {
            let value: (Server) -> String = { key == "protocol" ? $0.proto : key == "group" ? $0.group : $0.name }
            list.sort { a, b in
                let order = value(a).localizedStandardCompare(value(b))
                return sort.ascending ? order == .orderedAscending : order == .orderedDescending
            }
        }
        rows = list
        table.reloadData()
        if let chosen, let index = rows.firstIndex(where: { $0.id == chosen }) {
            table.selectRowIndexes([index], byExtendingSelection: false)
        }
        if Model.shared.view.servers.isEmpty {
            empty.stringValue = L("No servers yet. Copy a link in Throne and choose Import from Clipboard.")
        } else if rows.isEmpty {
            empty.stringValue = L("Nothing matches the search")
        } else {
            empty.stringValue = ""
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard let column = tableColumn else { return nil }
        let server = rows[row]
        let cell = NSTableCellView()
        if column.identifier.rawValue == "mark" {
            let image = NSImageView()
            if server.id == Model.shared.view.selected {
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
        let text: String
        switch column.identifier.rawValue {
        case "protocol": text = server.proto
        case "group": text = server.group
        default: text = server.name
        }
        let label = NSTextField(labelWithString: text)
        label.lineBreakMode = .byTruncatingTail
        if column.identifier.rawValue != "name" { label.textColor = .secondaryLabelColor }
        if server.id == Model.shared.view.selected, column.identifier.rawValue == "name" {
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

    func tableView(_ tableView: NSTableView, sortDescriptorsDidChange oldDescriptors: [NSSortDescriptor]) { reload() }

    func controlTextDidChange(_ obj: Notification) { reload() }

    /// The row an action is about: the one under a right-click, else the selected one.
    private var actionRow: Server? {
        let row = table.clickedRow >= 0 ? table.clickedRow : table.selectedRow
        return row >= 0 && row < rows.count ? rows[row] : nil
    }

    func validateMenuItem(_ menuItem: NSMenuItem) -> Bool {
        switch menuItem.action {
        case #selector(useSelected(_:)), #selector(removeSelected(_:)): return actionRow != nil
        default: return true
        }
    }

    @objc func useSelected(_ sender: Any?) {
        if let server = actionRow { Model.shared.select(server: server.id) }
    }

    @objc func removeSelected(_ sender: Any?) {
        if let server = actionRow { Model.shared.remove(server: server.id) }
    }

    @objc func focusSearch(_ sender: Any?) {
        view.window?.makeFirstResponder(search)
    }

    @objc func importFromClipboard(_ sender: Any?) {
        guard let text = NSPasteboard.general.string(forType: .string), !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else {
            report(title: L("The clipboard has no text"), text: L("Copy a server link first: in Throne, right-click a profile and copy it as a link."))
            return
        }
        importText(text)
    }

    @objc func importFromFile(_ sender: Any?) {
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
