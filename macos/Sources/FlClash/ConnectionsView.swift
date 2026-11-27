import AppKit

extension Notification.Name {
    /// The connections page was paused or let go.
    static let connectionsPaused = Notification.Name("FlClashConnectionsPaused")
}

/// What goes through the core right now: where to, by which rule, how much.
final class ConnectionsViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate {
    private let table = NSTableView()
    private let empty = EmptyStateView(symbol: "arrow.left.arrow.right")
    private var rows: [Connection] = []

    /// The list as it was when the person paused it; nil while it follows the core.
    private var frozen: [Connection]?

    /// A paused list stays as it is, to be read and acted on; the counters in
    /// the header go on.
    var paused: Bool {
        get { frozen != nil }
        set {
            guard newValue != paused else { return }
            frozen = newValue ? (Model.shared.status.running ? (Model.shared.stats?.connections ?? []) : []) : nil
            if isViewLoaded { reload() }
            NotificationCenter.default.post(name: .connectionsPaused, object: self)
        }
    }

    /// What the search field of the toolbar holds.
    var query = "" {
        didSet { if isViewLoaded, query != oldValue { reload() } }
    }

    override func loadView() {
        let root = NSView()
        for (id, title, width) in [("host", L("Destination"), 250.0), ("via", L("Through"), 110.0), ("rule", L("Rule"), 170.0), ("down", "↓", 70.0), ("up", "↑", 70.0), ("age", L("Open For"), 70.0)] {
            let column = NSTableColumn(identifier: .init(id))
            column.title = title
            column.width = width
            column.minWidth = 50
            table.addTableColumn(column)
        }
        table.style = .inset
        table.rowSizeStyle = .medium
        table.usesAlternatingRowBackgroundColors = false
        table.allowsMultipleSelection = false
        table.columnAutoresizingStyle = .uniformColumnAutoresizingStyle
        table.dataSource = self
        table.delegate = self
        table.setAccessibilityLabel(L("Connections"))
        let menu = NSMenu()
        menu.addItem(withTitle: L("Close Connection"), action: #selector(closeClicked), keyEquivalent: "").target = self
        table.menu = menu

        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.hasVerticalScroller = true
        scroll.drawsBackground = false
        scroll.borderType = .noBorder

        empty.button.isHidden = true
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
        NotificationCenter.default.addObserver(self, selector: #selector(reload), name: .statsChanged, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(reload), name: .modelChanged, object: nil)
        reload()
    }

    override func viewDidLayout() {
        super.viewDidLayout()
        table.sizeToFit()
    }

    @objc private func reload() {
        // The list changes every second; nothing to do while nobody looks.
        guard view.window != nil || rows.isEmpty else { return }
        let chosen = table.selectedRow >= 0 && table.selectedRow < rows.count ? rows[table.selectedRow].id : nil
        // Nothing to hold once the tunnel is off.
        if frozen != nil, !Model.shared.status.running { paused = false; return }
        let all = frozen ?? (Model.shared.status.running ? (Model.shared.stats?.connections ?? []) : [])
        let needle = query.trimmingCharacters(in: .whitespaces).lowercased()
        rows = needle.isEmpty ? all : all.filter { ($0.host + " " + $0.via + " " + $0.rule).lowercased().contains(needle) }
        table.reloadData()
        if let chosen, let index = rows.firstIndex(where: { $0.id == chosen }) {
            table.selectRowIndexes([index], byExtendingSelection: false)
        }

        table.enclosingScrollView?.isHidden = rows.isEmpty
        if !Model.shared.status.running {
            empty.show(title: L("Nothing runs"), text: L("Connections appear here while FlClash is on."), button: false)
        } else if all.isEmpty {
            empty.show(title: L("No open connections"), text: "", button: false)
        } else if rows.isEmpty {
            empty.show(title: L("Nothing matches the search"), text: "", button: false)
        } else {
            empty.isHidden = true
        }
    }

    @objc private func closeClicked() {
        let row = table.clickedRow >= 0 ? table.clickedRow : table.selectedRow
        guard row >= 0, row < rows.count else { return }
        let id = rows[row].id
        Model.shared.close(connection: id)
        // A paused list does not hear of it: the row goes now.
        if frozen != nil {
            frozen?.removeAll { $0.id == id }
            reload()
        }
    }

    func numberOfRows(in tableView: NSTableView) -> Int { rows.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        guard let column = tableColumn else { return nil }
        let connection = rows[row]
        let text: String
        var numeric = true
        switch column.identifier.rawValue {
        case "via": text = connection.via; numeric = false
        case "rule": text = ruleTitle(connection.rule); numeric = false
        case "down": text = amount(connection.download)
        case "up": text = amount(connection.upload)
        case "age": text = age(connection.start)
        default:
            // The port says something only when it is not the usual one.
            let usual = connection.port == 443 || connection.port == 80
            text = usual ? connection.host : connection.host + ":" + String(connection.port)
            numeric = false
        }
        let cell = textCell(text, secondary: column.identifier.rawValue != "host", strong: false)
        if column.identifier.rawValue == "via" {
            // What goes through the server, what goes past it, what is refused.
            let label = (cell as? NSTableCellView)?.textField
            switch connection.via {
            case "DIRECT": label?.stringValue = L("directly")
            case "REJECT", "REJECT-DROP":
                label?.stringValue = L("refused")
                label?.textColor = .systemRed
            default: label?.textColor = .labelColor
            }
        }
        if numeric {
            (cell as? NSTableCellView)?.textField?.font = .monospacedDigitSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)
        }
        if column.identifier.rawValue == "host" { cell.toolTip = connection.network.uppercased() + " " + connection.host + ":" + String(connection.port) }
        return cell
    }

    /// A rule the way a person would say it, not the way the core names it.
    private func ruleTitle(_ rule: String) -> String {
        let (kind, payload) = { () -> (String, String) in
            let parts = rule.split(separator: ",", maxSplits: 1).map(String.init)
            return (parts.first ?? "", parts.count > 1 ? parts[1] : "")
        }()
        switch kind {
        case "Match": return L("everything else")
        case "DomainSuffix": return "." + payload
        case "Domain", "IPCIDR", "IPCIDR6": return payload
        case "DomainKeyword": return "*" + payload + "*"
        case "RuleSet":
            let name = payload.replacingOccurrences(of: "geosite-", with: "").replacingOccurrences(of: "geoip-", with: "")
            return String(format: L("list “%@”"), name)
        default: return rule
        }
    }

    private func age(_ start: Int) -> String {
        let seconds = max(0, Int(Date().timeIntervalSince1970) - start)
        if seconds < 60 { return String(format: L("%d s"), seconds) }
        if seconds < 3600 { return String(format: L("%d min"), seconds / 60) }
        return String(format: L("%d h"), seconds / 3600)
    }
}
