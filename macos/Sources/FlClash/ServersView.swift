import AppKit
import UniformTypeIdentifiers

/// The servers: one table, filled by importing, pasting or dropping links.
final class ServersViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate, NSMenuItemValidation, NSMenuDelegate {
    /// A row is a server or the heading of a group of them.
    private enum Row {
        case group(String)
        case server(Server)
    }

    private let table = ListTableView()
    private let empty = EmptyStateView(symbol: "server.rack")
    private var rows: [Row] = []

    private func server(at index: Int) -> Server? {
        guard index >= 0, index < rows.count, case .server(let server) = rows[index] else { return nil }
        return server
    }

    private var shownServers: [Server] {
        rows.compactMap { if case .server(let server) = $0 { return server } else { return nil } }
    }

    /// What the search field of the toolbar holds.
    var query = "" {
        didSet { if isViewLoaded, query != oldValue { reload() } }
    }

    /// The order of the servers inside a group.
    enum Order: String, CaseIterable {
        case added, name, latency

        var title: String {
            switch self {
            case .added: return L("As Added")
            case .name: return L("By Name")
            case .latency: return L("By Latency")
            }
        }
    }

    var order = Order(rawValue: UserDefaults.standard.string(forKey: "serverOrder") ?? "") ?? .added {
        didSet {
            UserDefaults.standard.set(order.rawValue, forKey: "serverOrder")
            if isViewLoaded { reload() }
        }
    }

    override func loadView() {
        let root = DropView()
        root.onDrop = { [weak self] text, _ in self?.importText(text) }

        // A list, not a sheet of data: no header, the order is chosen in the toolbar.
        for (id, width) in [("mark", 26.0), ("name", 260.0), ("protocol", 220.0), ("delay", 96.0)] {
            let column = NSTableColumn(identifier: .init(id))
            column.width = width
            column.minWidth = id == "mark" ? 26 : 60
            if id == "mark" { column.maxWidth = 26 }
            if id == "delay" { column.maxWidth = 110 }
            table.addTableColumn(column)
        }
        table.headerView = nil
        table.style = .inset
        table.rowHeight = 28
        table.intercellSpacing = NSSize(width: 8, height: 2)
        table.usesAlternatingRowBackgroundColors = false
        table.allowsMultipleSelection = true
        // The name takes what is left; the rest keep their width.
        table.columnAutoresizingStyle = .reverseSequentialColumnAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.doubleAction = #selector(connectClicked(_:))
        table.onDelete = { [weak self] in self?.delete(nil) }
        table.setAccessibilityLabel(L("Servers"))
        // The menu is made for the row under the click: a server or a group.
        let menu = NSMenu()
        menu.delegate = self
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
        let chosen = Set(table.selectedRowIndexes.compactMap { server(at: $0)?.id })
        let all = Model.shared.view.servers
        let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
        var list = all
        if !needle.isEmpty {
            list = list.filter { ($0.name + " " + $0.proto + " " + $0.group).lowercased().contains(needle) }
        }
        switch order {
        case .added:
            break
        case .name:
            list.sort { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
        case .latency:
            // Measured ones by the number; no answer after them; not measured last.
            let value: (Server) -> Int = {
                switch Model.shared.delays[$0.id] {
                case .ms(let ms): return ms
                case .failed: return Int.max - 1
                default: return Int.max
                }
            }
            // The sort keeps the order of servers that are equal, so the list
            // does not jump while results arrive.
            list = list.enumerated().sorted { a, b in
                value(a.element) != value(b.element) ? value(a.element) < value(b.element) : a.offset < b.offset
            }.map(\.element)
        }

        // Groups get headings once there is more than one, or one is a
        // subscription: its heading says when it was read and lets it be read again.
        let subscriptions = Model.shared.view.subscriptions.map(\.name)
        var groups = subscriptions
        for group in all.map(\.group) where !groups.contains(group) {
            // What was added by hand comes first.
            groups.insert(group, at: groups.count - subscriptions.count)
        }
        if groups.count > 1 || !subscriptions.isEmpty {
            rows = []
            for group in groups {
                let members = list.filter { $0.group == group }
                // While searching, a group with nothing found is not shown.
                if members.isEmpty, !needle.isEmpty || !subscriptions.contains(group) { continue }
                rows.append(.group(group))
                rows += members.map { .server($0) }
            }
        } else {
            rows = list.map { .server($0) }
        }
        table.reloadData()
        table.selectRowIndexes(IndexSet(rows.indices.filter { index in server(at: index).map { chosen.contains($0.id) } ?? false }), byExtendingSelection: false)

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

    func tableView(_ tableView: NSTableView, isGroupRow row: Int) -> Bool {
        if case .group = rows[row] { return true }
        return false
    }

    func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { server(at: row) != nil }

    func tableView(_ tableView: NSTableView, heightOfRow row: Int) -> CGFloat {
        if case .group = rows[row] { return 30 }
        return 28
    }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        if case .group(let name) = rows[row] { return groupCell(name) }
        guard let column = tableColumn, let server = server(at: row) else { return nil }
        let selected = server.id == Model.shared.view.selected
        if column.identifier.rawValue == "mark" { return markCell(selected) }
        if column.identifier.rawValue == "delay" { return delayCell(Model.shared.delays[server.id]) }
        let text: String
        switch column.identifier.rawValue {
        case "protocol":
            // The engine speaks English; this note is for the person.
            text = server.proto.replacingOccurrences(of: "certificate not checked", with: L("certificate not checked"))
        default: text = server.name
        }
        return textCell(text, secondary: column.identifier.rawValue != "name", strong: selected)
    }

    /// The heading of a group. For a subscription it says how fresh it is and
    /// what the provider told about the account, and has a button to read it again.
    private func groupCell(_ name: String) -> NSView {
        let cell = NSTableCellView()
        // "default" is where servers added by hand go; people need not see the word.
        let title = NSTextField(labelWithString: name == "default" ? L("Added by Hand") : name)
        title.font = .systemFont(ofSize: 12, weight: .semibold)
        title.textColor = .secondaryLabelColor
        var views: [NSView] = [title]
        if let subscription = Model.shared.view.subscriptions.first(where: { $0.name == name }) {
            let about = NSTextField(labelWithString: subscriptionAbout(subscription))
            about.font = .systemFont(ofSize: 11)
            about.textColor = subscription.problem.isEmpty ? .tertiaryLabelColor : .systemOrange
            about.lineBreakMode = .byTruncatingTail
            about.toolTip = subscription.problem.isEmpty ? subscription.host : subscription.problem
            about.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
            let spacer = NSView()
            spacer.setContentHuggingPriority(.init(1), for: .horizontal)
            views += [about, spacer]
            if Model.shared.updating.contains(name) {
                let spinner = NSProgressIndicator()
                spinner.style = .spinning
                spinner.controlSize = .small
                spinner.startAnimation(nil)
                views.append(spinner)
            } else {
                let refresh = NSButton(image: NSImage(systemSymbolName: "arrow.clockwise", accessibilityDescription: L("Update Subscription")) ?? NSImage(), target: self, action: #selector(updateClicked(_:)))
                refresh.isBordered = false
                refresh.toolTip = L("Update Subscription")
                refresh.identifier = .init(name)
                views.append(refresh)
            }
        }
        let line = NSStackView(views: views)
        line.orientation = .horizontal
        line.alignment = .centerY
        line.spacing = 8
        line.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(line)
        cell.textField = title
        NSLayoutConstraint.activate([
            line.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 6),
            line.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -10),
            line.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    private static let relative = RelativeDateTimeFormatter()
    private static let day: DateFormatter = {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .none
        return formatter
    }()

    private func subscriptionAbout(_ subscription: Subscription) -> String {
        if !subscription.problem.isEmpty { return L("The last update failed") }
        var parts: [String] = []
        if subscription.updated > 0 {
            let updated = Date(timeIntervalSince1970: TimeInterval(subscription.updated))
            if Date().timeIntervalSince(updated) < 60 {
                parts.append(L("updated just now"))
            } else {
                parts.append(String(format: L("updated %@"), Self.relative.localizedString(for: updated, relativeTo: Date())))
            }
        }
        if subscription.total > 0 {
            parts.append(String(format: L("%@ of %@ used"), amount(subscription.used), amount(subscription.total)))
        }
        if subscription.expire > 0 {
            parts.append(String(format: L("until %@"), Self.day.string(from: Date(timeIntervalSince1970: TimeInterval(subscription.expire)))))
        }
        return parts.joined(separator: "  ·  ")
    }

    @objc private func updateClicked(_ sender: NSButton) {
        if let name = sender.identifier?.rawValue { updateSubscription(name) }
    }

    private func updateSubscription(_ name: String) {
        Model.shared.updateSubscription(name) { [weak self] error in
            if let error { self?.report(title: String(format: L("The subscription “%@” was not updated"), name), text: error) }
        }
    }

    /// The menu of the row under the click.
    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()
        let clicked = table.clickedRow
        if clicked >= 0, clicked < rows.count, case .group(let name) = rows[clicked] {
            guard Model.shared.view.subscriptions.contains(where: { $0.name == name }) else { return }
            let update = menu.addItem(withTitle: L("Update Subscription"), action: #selector(updateFromMenu(_:)), keyEquivalent: "")
            update.target = self
            update.representedObject = name
            menu.addItem(.separator())
            let remove = menu.addItem(withTitle: L("Delete Subscription…"), action: #selector(removeSubscription(_:)), keyEquivalent: "")
            remove.target = self
            remove.representedObject = name
            return
        }
        menu.addItem(withTitle: L("Use This Server"), action: #selector(useSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(withTitle: L("Test Latency"), action: #selector(measureSelected(_:)), keyEquivalent: "").target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: L("Delete"), action: #selector(delete(_:)), keyEquivalent: "").target = self
    }

    @objc private func updateFromMenu(_ sender: NSMenuItem) {
        if let name = sender.representedObject as? String { updateSubscription(name) }
    }

    @objc private func removeSubscription(_ sender: NSMenuItem) {
        guard let name = sender.representedObject as? String, let window = view.window else { return }
        let alert = NSAlert()
        alert.messageText = String(format: L("Delete the subscription “%@”?"), name)
        alert.informativeText = L("Its servers will be deleted with it.")
        alert.addButton(withTitle: L("Delete")).hasDestructiveAction = true
        alert.addButton(withTitle: L("Cancel"))
        alert.beginSheetModal(for: window) { response in
            if response == .alertFirstButtonReturn { Model.shared.removeSubscription(name) }
        }
    }

    /// Asks for the address of a subscription and adds it.
    func addSubscription() {
        guard let window = view.window else { return }
        let address = NSTextField()
        address.placeholderString = "https://"
        let name = NSTextField()
        name.placeholderString = L("Name (optional)")
        let fields = NSStackView(views: [address, name])
        fields.orientation = .vertical
        fields.spacing = 8
        fields.frame = NSRect(x: 0, y: 0, width: 320, height: 56)
        for field in [address, name] { field.widthAnchor.constraint(equalToConstant: 320).isActive = true }
        // A copied address is most likely the one to add.
        if let copied = NSPasteboard.general.string(forType: .string)?.trimmingCharacters(in: .whitespacesAndNewlines),
           copied.hasPrefix("http://") || copied.hasPrefix("https://"), !copied.contains("\n") {
            address.stringValue = copied
        }
        let alert = NSAlert()
        alert.messageText = L("Add a Subscription")
        alert.informativeText = L("The address your provider gave you. FlClash reads the list of servers from it and keeps it up to date.")
        alert.accessoryView = fields
        alert.addButton(withTitle: L("Add"))
        alert.addButton(withTitle: L("Cancel"))
        alert.window.initialFirstResponder = address
        alert.beginSheetModal(for: window) { [weak self] response in
            guard response == .alertFirstButtonReturn else { return }
            Model.shared.addSubscription(name: name.stringValue, address: address.stringValue) { result, error in
                guard let self else { return }
                let failed = result?.failed ?? []
                if error == nil, failed.isEmpty { return }
                var lines = failed.prefix(8).map { String(format: L("Entry %d: %@"), $0.entry, $0.reason) }
                if let error { lines.insert(error, at: 0) }
                let title = error == nil
                    ? String(format: L("Imported %d, not imported %d"), result?.imported.count ?? 0, failed.count)
                    : L("The subscription was not added")
                self.report(title: title, text: lines.joined(separator: "\n\n"))
            }
        }
    }

    /// The rows an action is about: the one under a right-click when it is
    /// outside the selection, else the selection.
    private var actionRows: [Server] {
        let clicked = table.clickedRow
        let indexes = clicked >= 0 && !table.selectedRowIndexes.contains(clicked)
            ? IndexSet(integer: clicked) : table.selectedRowIndexes
        return indexes.compactMap { server(at: $0) }
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

    /// A double click is the whole wish: this server, now.
    @objc private func connectClicked(_ sender: Any?) {
        guard let server = server(at: table.clickedRow), Model.shared.helperReady, !Model.shared.busy else { return }
        Model.shared.connect(server: server.id)
    }

    @objc private func measureSelected(_ sender: Any?) {
        Model.shared.measure(servers: actionRows.map(\.id))
    }

    /// Measures every server the table shows, in its order.
    func measureAll() {
        Model.shared.measure(servers: shownServers.map(\.id))
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
        image.image = NSImage(systemSymbolName: "checkmark.circle.fill", accessibilityDescription: L("Selected"))
        image.symbolConfiguration = .init(pointSize: 14, weight: .medium)
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

/// The colour of a delay: quick, bearable, slow. The number is one round trip
/// to a site through the server over a connection that is already up.
func delayColor(_ ms: Int) -> NSColor {
    if ms < 150 { return .systemGreen }
    if ms < 400 { return .systemYellow }
    return .systemOrange
}

/// The cell of the latency column: a coloured dot and a number, a spinner
/// while it is on its way, or the word that there was no answer, with the
/// reason on hover.
func delayCell(_ delay: Model.Delay?) -> NSView {
    let cell = NSTableCellView()
    var views: [NSView] = []
    switch delay {
    case .ms(let ms):
        let dot = NSImageView(image: NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil) ?? NSImage())
        dot.symbolConfiguration = .init(pointSize: 6, weight: .regular)
        dot.contentTintColor = delayColor(ms)
        let label = NSTextField(labelWithString: String(format: L("%d ms"), ms))
        label.font = .monospacedDigitSystemFont(ofSize: 12, weight: .regular)
        label.textColor = .secondaryLabelColor
        cell.textField = label
        views = [dot, label]
    case .failed(let reason):
        let dot = NSImageView(image: NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil) ?? NSImage())
        dot.symbolConfiguration = .init(pointSize: 6, weight: .regular)
        dot.contentTintColor = .systemRed
        let label = NSTextField(labelWithString: L("No answer"))
        label.font = .systemFont(ofSize: 12)
        label.textColor = .secondaryLabelColor
        cell.textField = label
        cell.toolTip = reason
        views = [dot, label]
    case .waiting:
        let spinner = NSProgressIndicator()
        spinner.style = .spinning
        spinner.controlSize = .small
        spinner.startAnimation(nil)
        views = [spinner]
    case nil:
        return cell
    }
    let line = NSStackView(views: views)
    line.orientation = .horizontal
    line.alignment = .centerY
    line.spacing = 6
    line.translatesAutoresizingMaskIntoConstraints = false
    cell.addSubview(line)
    NSLayoutConstraint.activate([
        line.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 2),
        line.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
    ])
    return cell
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
