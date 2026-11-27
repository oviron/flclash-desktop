import AppKit

/// The panel under the menu bar item: the switch, what runs, and the choices
/// people change most, without opening the window.
final class PanelViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate {
    private let openWindow: () -> Void
    private let openSettings: () -> Void

    private let badge = StatusBadge(side: 34)
    private let headline = NSTextField(labelWithString: "")
    private let detail = NSTextField(labelWithString: "")
    private let power = NSSwitch()
    private let graph = TrafficGraph()
    private let down = NSTextField(labelWithString: "")
    private let up = NSTextField(labelWithString: "")
    private let traffic = NSStackView()
    private let modes = NSSegmentedControl(labels: [L("Port Only"), L("System Proxy"), L("TUN")], trackingMode: .selectOne, target: nil, action: nil)
    private let policies = NSPopUpButton(frame: .zero, pullsDown: false)
    private let notice = NSTextField(wrappingLabelWithString: "")
    private let unblock = NSButton(title: L("Unblock"), target: nil, action: nil)
    private let noticeRow = NSStackView()
    private let table = NSTableView()
    private let scroll = NSScrollView()
    private var tableHeight: NSLayoutConstraint?
    private var servers: [Server] = []

    private static let width: CGFloat = 320
    private static let rowHeight: CGFloat = 26

    init(openWindow: @escaping () -> Void, openSettings: @escaping () -> Void) {
        self.openWindow = openWindow
        self.openSettings = openSettings
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    override func loadView() {
        headline.font = .systemFont(ofSize: 14, weight: .semibold)
        headline.lineBreakMode = .byTruncatingTail
        detail.font = .systemFont(ofSize: 11)
        detail.textColor = .secondaryLabelColor
        detail.lineBreakMode = .byTruncatingTail
        for label in [headline, detail] { label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal) }
        let titles = NSStackView(views: [headline, detail])
        titles.orientation = .vertical
        titles.alignment = .leading
        titles.spacing = 2
        power.target = self
        power.action = #selector(toggle)
        let top = row([badge, titles, spacer(), power], spacing: 10)

        for label in [down, up] {
            label.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
            label.textColor = .secondaryLabelColor
        }
        graph.setContentHuggingPriority(.init(1), for: .horizontal)
        graph.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let rates = NSStackView(views: [down, up])
        rates.orientation = .vertical
        rates.alignment = .leading
        rates.spacing = 2
        rates.widthAnchor.constraint(equalToConstant: 84).isActive = true
        traffic.orientation = .horizontal
        traffic.alignment = .centerY
        traffic.spacing = 10
        traffic.setViews([graph, rates], in: .leading)

        modes.target = self
        modes.action = #selector(modeChanged)
        modes.segmentDistribution = .fillEqually
        policies.target = self
        policies.action = #selector(policyChanged)
        let routing = NSTextField(labelWithString: L("Routing"))
        routing.textColor = .secondaryLabelColor
        routing.setContentCompressionResistancePriority(.required, for: .horizontal)
        policies.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        let policyRow = row([routing, spacer(), policies], spacing: 8)

        notice.font = .systemFont(ofSize: 11)
        notice.textColor = .secondaryLabelColor
        notice.preferredMaxLayoutWidth = Self.width - 28 - 120
        unblock.bezelStyle = .push
        unblock.controlSize = .small
        unblock.target = self
        unblock.action = #selector(turnOff)
        unblock.setContentHuggingPriority(.required, for: .horizontal)
        noticeRow.orientation = .horizontal
        noticeRow.alignment = .centerY
        noticeRow.spacing = 8
        noticeRow.setViews([notice, unblock], in: .leading)

        let column = NSTableColumn(identifier: .init("server"))
        table.addTableColumn(column)
        table.headerView = nil
        table.style = .inset
        table.rowHeight = Self.rowHeight
        table.intercellSpacing = NSSize(width: 0, height: 2)
        table.backgroundColor = .clear
        table.allowsEmptySelection = true
        table.dataSource = self
        table.delegate = self
        table.target = self
        table.action = #selector(serverClicked)
        scroll.documentView = table
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        scroll.borderType = .noBorder

        let open = NSButton(title: L("Open FlClash"), target: self, action: #selector(openClicked))
        open.bezelStyle = .push
        let settings = iconButton("gearshape", L("Settings…"), #selector(settingsClicked))
        let quit = iconButton("power", L("Quit FlClash"), #selector(quitClicked))
        let footer = row([open, spacer(), settings, quit], spacing: 6)

        let stack = NSStackView(views: [top, traffic, modes, policyRow, noticeRow, line(), scroll, line(), footer])
        stack.orientation = .vertical
        stack.alignment = .leading
        stack.spacing = 12
        stack.translatesAutoresizingMaskIntoConstraints = false
        let root = NSView()
        root.addSubview(stack)
        let height = scroll.heightAnchor.constraint(equalToConstant: 0)
        tableHeight = height
        NSLayoutConstraint.activate([
            root.widthAnchor.constraint(equalToConstant: Self.width),
            stack.topAnchor.constraint(equalTo: root.topAnchor, constant: 14),
            stack.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 14),
            stack.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -14),
            stack.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -12),
            height,
        ])
        for view in stack.arrangedSubviews where view !== scroll {
            view.widthAnchor.constraint(equalTo: stack.widthAnchor).isActive = true
        }
        // The list runs to the edges of the panel, as lists in menus do.
        scroll.widthAnchor.constraint(equalTo: root.widthAnchor).isActive = true
        scroll.centerXAnchor.constraint(equalTo: root.centerXAnchor).isActive = true
        stack.setCustomSpacing(6, after: stack.arrangedSubviews[5])
        stack.setCustomSpacing(6, after: scroll)
        view = root
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        NotificationCenter.default.addObserver(self, selector: #selector(update), name: .modelChanged, object: nil)
        NotificationCenter.default.addObserver(self, selector: #selector(updateTraffic), name: .statsChanged, object: nil)
        update()
    }

    private func row(_ views: [NSView], spacing: CGFloat) -> NSStackView {
        let stack = NSStackView(views: views)
        stack.orientation = .horizontal
        stack.alignment = .centerY
        stack.spacing = spacing
        return stack
    }

    private func spacer() -> NSView {
        let view = NSView()
        view.setContentHuggingPriority(.init(1), for: .horizontal)
        return view
    }

    private func line() -> NSView {
        let box = NSBox()
        box.boxType = .separator
        return box
    }

    private func iconButton(_ symbol: String, _ name: String, _ action: Selector) -> NSButton {
        let button = NSButton(image: NSImage(systemSymbolName: symbol, accessibilityDescription: name) ?? NSImage(), target: self, action: action)
        button.bezelStyle = .push
        button.toolTip = name
        return button
    }

    @objc private func updateTraffic() {
        guard let stats = Model.shared.stats, Model.shared.status.running else {
            traffic.isHidden = true
            return
        }
        traffic.isHidden = false
        graph.samples = Model.shared.history
        down.stringValue = "↓ " + rate(stats.down)
        up.stringValue = "↑ " + rate(stats.up)
    }

    @objc private func update() {
        let model = Model.shared
        let status = model.status
        let look = Look.current
        badge.show(look)
        graph.tint = look.tint
        headline.stringValue = status.headline

        let server = status.running || status.blocked ? status.server : (model.selectedServer?.name ?? "")
        if !model.helperReady {
            detail.stringValue = L("Open FlClash to set it up")
        } else if server.isEmpty {
            detail.stringValue = L("Choose a server to connect")
        } else if status.running, status.guard_ == "full" {
            detail.stringValue = server + "  ·  " + L("protected")
        } else {
            detail.stringValue = server
        }

        power.state = status.running ? .on : .off
        power.isEnabled = model.helperReady && !model.busy && (status.running || model.selectedServer != nil)

        let mode = status.running && !status.mode.isEmpty ? status.mode : model.view.mode
        modes.selectedSegment = ["proxy": 0, "system": 1, "tun": 2][mode] ?? 0
        modes.isEnabled = model.helperReady && !model.busy

        let policy = status.running && !status.policy.isEmpty ? status.policy : model.view.policy
        policies.removeAllItems()
        for item in model.view.policies {
            policies.addItem(withTitle: policyTitle(item.name))
            policies.lastItem?.representedObject = item.name
        }
        if let index = model.view.policies.firstIndex(where: { $0.name == policy }) { policies.selectItem(at: index) }
        policies.isEnabled = !model.busy

        if model.helperReady, status.blocked {
            notice.stringValue = status.mode == "system"
                ? L("Apps that follow the system proxy are offline until it is back.")
                : L("The internet is off until the tunnel is back.")
            unblock.isEnabled = !model.busy
            noticeRow.isHidden = false
        } else {
            noticeRow.isHidden = true
        }

        servers = model.view.servers
        table.reloadData()
        // The list grows with the servers up to eight rows, then scrolls.
        let rows = CGFloat(min(max(servers.count, 1), 8))
        tableHeight?.constant = rows * (Self.rowHeight + 2) + 4
        updateTraffic()
    }

    func numberOfRows(in tableView: NSTableView) -> Int { max(servers.count, 1) }

    func tableView(_ tableView: NSTableView, shouldSelectRow row: Int) -> Bool { false }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let cell = NSTableCellView()
        guard row < servers.count else {
            let none = NSTextField(labelWithString: L("No servers yet"))
            none.textColor = .secondaryLabelColor
            none.translatesAutoresizingMaskIntoConstraints = false
            cell.addSubview(none)
            NSLayoutConstraint.activate([
                none.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 26),
                none.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
            ])
            return cell
        }
        let server = servers[row]
        let chosen = server.id == Model.shared.view.selected
        let check = NSImageView()
        if chosen {
            check.image = NSImage(systemSymbolName: "checkmark", accessibilityDescription: L("Selected"))
            check.symbolConfiguration = .init(pointSize: 11, weight: .semibold)
            check.contentTintColor = .controlAccentColor
        }
        check.widthAnchor.constraint(equalToConstant: 16).isActive = true
        let name = NSTextField(labelWithString: server.name)
        name.lineBreakMode = .byTruncatingTail
        name.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        if chosen { name.font = .systemFont(ofSize: NSFont.systemFontSize, weight: .semibold) }
        let gap = NSView()
        gap.setContentHuggingPriority(.init(1), for: .horizontal)
        var views: [NSView] = [check, name, gap]
        if case .ms(let ms) = Model.shared.delays[server.id] {
            let dot = NSImageView(image: NSImage(systemSymbolName: "circle.fill", accessibilityDescription: nil) ?? NSImage())
            dot.symbolConfiguration = .init(pointSize: 6, weight: .regular)
            dot.contentTintColor = delayColor(ms)
            let label = NSTextField(labelWithString: String(format: L("%d ms"), ms))
            label.font = .monospacedDigitSystemFont(ofSize: 11, weight: .regular)
            label.textColor = .secondaryLabelColor
            views += [dot, label]
        }
        let line = NSStackView(views: views)
        line.orientation = .horizontal
        line.alignment = .centerY
        line.spacing = 6
        line.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(line)
        cell.textField = name
        NSLayoutConstraint.activate([
            line.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 4),
            line.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -6),
            line.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    /// A click on a server chooses it; a tunnel that is up moves to it.
    @objc private func serverClicked() {
        let row = table.clickedRow
        guard row >= 0, row < servers.count, !Model.shared.busy else { return }
        Model.shared.select(server: servers[row].id)
    }

    @objc private func toggle() { Model.shared.toggle() }
    @objc private func turnOff() { Model.shared.turnOff() }

    @objc private func modeChanged() {
        Model.shared.setMode(["proxy", "system", "tun"][max(0, min(2, modes.selectedSegment))])
    }

    @objc private func policyChanged() {
        if let name = policies.selectedItem?.representedObject as? String, name != Model.shared.view.policy {
            Model.shared.select(policy: name)
        }
    }

    @objc private func openClicked() { openWindow() }
    @objc private func settingsClicked() { openSettings() }
    @objc private func quitClicked() { NSApp.terminate(nil) }
}
