import AppKit

private extension NSToolbarItem.Identifier {
    static let measure = NSToolbarItem.Identifier("measure")
    static let addServers = NSToolbarItem.Identifier("addServers")
    static let addPolicy = NSToolbarItem.Identifier("addPolicy")
    static let search = NSToolbarItem.Identifier("search")
}

/// The window: a sidebar, a toolbar that follows the page, and the page.
///
/// What the menus and the toolbar do goes through here, so it works wherever
/// the keyboard focus is.
final class MainWindowController: NSWindowController, NSToolbarDelegate, NSSearchFieldDelegate {
    private let root = RootSplitViewController()
    private var searchItem: NSSearchToolbarItem?

    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 860, height: 540),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered, defer: false
        )
        window.toolbarStyle = .unified
        window.minSize = NSSize(width: 640, height: 400)
        window.isReleasedWhenClosed = false
        window.contentViewController = root
        super.init(window: window)

        let toolbar = NSToolbar(identifier: "FlClashMainToolbar")
        toolbar.delegate = self
        toolbar.displayMode = .iconOnly
        window.toolbar = toolbar

        // The size is set last: taking the content and the toolbar resizes the
        // window, and that must not be remembered as the person's choice.
        window.setContentSize(NSSize(width: 860, height: 540))
        window.center()
        window.setFrameAutosaveName("FlClashMainWindow")

        root.onPage = { [weak self] in self?.pageChanged() }
        NotificationCenter.default.addObserver(self, selector: #selector(pageChanged), name: .modelChanged, object: nil)
        pageChanged()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    @objc private func pageChanged() {
        guard let window else { return }
        let page = root.page
        window.title = page.title
        switch page {
        case .servers:
            let count = Model.shared.view.servers.count
            window.subtitle = count == 0 ? "" : String.localizedStringWithFormat(L("%d servers"), count)
        case .routing:
            window.subtitle = ""
        }
        for item in window.toolbar?.items ?? [] {
            switch item.itemIdentifier {
            case .measure, .addServers, .search: item.isHidden = page != .servers
            case .addPolicy: item.isHidden = page != .routing
            default: break
            }
        }
    }

    // MARK: Toolbar

    func toolbarDefaultItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        [.sidebarTrackingSeparator, .flexibleSpace, .measure, .addServers, .addPolicy, .search]
    }

    func toolbarAllowedItemIdentifiers(_ toolbar: NSToolbar) -> [NSToolbarItem.Identifier] {
        toolbarDefaultItemIdentifiers(toolbar)
    }

    func toolbar(_ toolbar: NSToolbar, itemForItemIdentifier id: NSToolbarItem.Identifier, willBeInsertedIntoToolbar flag: Bool) -> NSToolbarItem? {
        switch id {
        case .measure:
            let item = NSToolbarItem(itemIdentifier: id)
            item.image = NSImage(systemSymbolName: "speedometer", accessibilityDescription: L("Test Latency"))
            item.label = L("Test Latency")
            item.toolTip = L("Test Latency")
            item.isBordered = true
            item.target = self
            item.action = #selector(testLatency(_:))
            item.isHidden = root.page != .servers
            return item
        case .addServers:
            let item = NSMenuToolbarItem(itemIdentifier: id)
            item.image = NSImage(systemSymbolName: "plus", accessibilityDescription: L("Add servers"))
            item.label = L("Add servers")
            item.toolTip = L("Add servers")
            let menu = NSMenu()
            menu.addItem(withTitle: L("Import from Clipboard"), action: #selector(importFromClipboard(_:)), keyEquivalent: "").target = self
            menu.addItem(withTitle: L("Import from File…"), action: #selector(importFromFile(_:)), keyEquivalent: "").target = self
            item.menu = menu
            item.isHidden = root.page != .servers
            return item
        case .addPolicy:
            let item = NSToolbarItem(itemIdentifier: id)
            item.image = NSImage(systemSymbolName: "plus", accessibilityDescription: L("Import a policy"))
            item.label = L("Import a policy")
            item.toolTip = L("Import a policy from a rules file")
            item.isBordered = true
            item.target = self
            item.action = #selector(importPolicy(_:))
            item.isHidden = root.page != .routing
            return item
        case .search:
            let item = NSSearchToolbarItem(itemIdentifier: id)
            item.searchField.placeholderString = L("Search")
            item.searchField.sendsSearchStringImmediately = true
            item.searchField.delegate = self
            item.preferredWidthForSearchField = 220
            item.isHidden = root.page != .servers
            searchItem = item
            return item
        default:
            return nil
        }
    }

    func controlTextDidChange(_ obj: Notification) {
        guard let field = obj.object as? NSSearchField else { return }
        root.content.servers.query = field.stringValue
    }

    // MARK: Actions of the menus and the toolbar

    @objc func showServers(_ sender: Any?) { root.show(.servers) }
    @objc func showRouting(_ sender: Any?) { root.show(.routing) }

    @objc func importFromClipboard(_ sender: Any?) {
        root.show(.servers)
        root.content.servers.importFromClipboard()
    }

    /// Edit > Paste outside a text field: a copied link becomes a server.
    @objc func paste(_ sender: Any?) {
        if root.page == .servers { root.content.servers.importFromClipboard() } else { NSSound.beep() }
    }

    @objc func importFromFile(_ sender: Any?) {
        root.show(.servers)
        root.content.servers.importFromFile()
    }

    @objc func testLatency(_ sender: Any?) {
        root.show(.servers)
        root.content.servers.measureAll()
    }

    @objc func importPolicy(_ sender: Any?) {
        root.show(.routing)
        root.content.routing.importPolicy()
    }

    @objc func focusSearch(_ sender: Any?) {
        root.show(.servers)
        searchItem?.beginSearchInteraction()
    }
}

/// A sidebar with the two things the app has, and the chosen one beside it.
final class RootSplitViewController: NSSplitViewController {
    private let sidebar = SidebarViewController()
    let content = ContentViewController()
    var onPage: (() -> Void)?

    var page: Page { content.page }

    override func viewDidLoad() {
        super.viewDidLoad()
        let side = NSSplitViewItem(sidebarWithViewController: sidebar)
        side.minimumThickness = 170
        side.maximumThickness = 240
        side.canCollapse = false
        addSplitViewItem(side)
        let main = NSSplitViewItem(viewController: content)
        main.minimumThickness = 440
        addSplitViewItem(main)
        sidebar.onSelect = { [weak self] page in
            self?.content.show(page)
            self?.onPage?()
        }
    }

    func show(_ page: Page) {
        if page != self.page { sidebar.select(page) }
    }
}

enum Page: Int, CaseIterable {
    case servers, routing

    var title: String {
        switch self {
        case .servers: return L("Servers")
        case .routing: return L("Routing")
        }
    }

    var symbol: String {
        switch self {
        case .servers: return "server.rack"
        case .routing: return "arrow.triangle.branch"
        }
    }
}

final class SidebarViewController: NSViewController, NSTableViewDataSource, NSTableViewDelegate {
    var onSelect: ((Page) -> Void)?
    private let table = NSTableView()

    override func loadView() {
        let column = NSTableColumn(identifier: .init("page"))
        table.addTableColumn(column)
        table.headerView = nil
        table.style = .sourceList
        table.rowSizeStyle = .medium
        table.dataSource = self
        table.delegate = self
        table.allowsEmptySelection = false
        let scroll = NSScrollView()
        scroll.documentView = table
        scroll.drawsBackground = false
        scroll.hasVerticalScroller = true
        view = scroll
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        table.reloadData()
        table.selectRowIndexes([0], byExtendingSelection: false)
    }

    func select(_ page: Page) {
        table.selectRowIndexes([page.rawValue], byExtendingSelection: false)
    }

    func numberOfRows(in tableView: NSTableView) -> Int { Page.allCases.count }

    func tableView(_ tableView: NSTableView, viewFor tableColumn: NSTableColumn?, row: Int) -> NSView? {
        let page = Page.allCases[row]
        let cell = NSTableCellView()
        let image = NSImageView(image: NSImage(systemSymbolName: page.symbol, accessibilityDescription: nil) ?? NSImage())
        let label = NSTextField(labelWithString: page.title)
        image.translatesAutoresizingMaskIntoConstraints = false
        label.translatesAutoresizingMaskIntoConstraints = false
        cell.addSubview(image)
        cell.addSubview(label)
        cell.imageView = image
        cell.textField = label
        NSLayoutConstraint.activate([
            image.leadingAnchor.constraint(equalTo: cell.leadingAnchor, constant: 2),
            image.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
            image.widthAnchor.constraint(equalToConstant: 20),
            label.leadingAnchor.constraint(equalTo: image.trailingAnchor, constant: 6),
            label.trailingAnchor.constraint(equalTo: cell.trailingAnchor, constant: -4),
            label.centerYAnchor.constraint(equalTo: cell.centerYAnchor),
        ])
        return cell
    }

    func tableViewSelectionDidChange(_ notification: Notification) {
        if let page = Page(rawValue: table.selectedRow) { onSelect?(page) }
    }
}

/// The header that is always there, and one page under it.
final class ContentViewController: NSViewController {
    private let header = HeaderView()
    private let container = NSView()
    private var current: NSViewController?
    private(set) var page = Page.servers
    private(set) lazy var servers = ServersViewController()
    private(set) lazy var routing = RoutingViewController()

    override func loadView() {
        let root = NSView()
        header.translatesAutoresizingMaskIntoConstraints = false
        container.translatesAutoresizingMaskIntoConstraints = false
        root.addSubview(header)
        root.addSubview(container)
        NSLayoutConstraint.activate([
            header.topAnchor.constraint(equalTo: root.safeAreaLayoutGuide.topAnchor),
            header.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            header.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            container.topAnchor.constraint(equalTo: header.bottomAnchor),
            container.leadingAnchor.constraint(equalTo: root.leadingAnchor),
            container.trailingAnchor.constraint(equalTo: root.trailingAnchor),
            container.bottomAnchor.constraint(equalTo: root.bottomAnchor),
        ])
        view = root
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        show(.servers)
    }

    func show(_ page: Page) {
        let next: NSViewController = page == .servers ? servers : routing
        guard next !== current else { return }
        self.page = page
        current?.view.removeFromSuperview()
        current?.removeFromParent()
        addChild(next)
        next.view.translatesAutoresizingMaskIntoConstraints = false
        container.addSubview(next.view)
        NSLayoutConstraint.activate([
            next.view.topAnchor.constraint(equalTo: container.topAnchor),
            next.view.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            next.view.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            next.view.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])
        current = next
    }
}

/// A page that takes what is dropped on it: links as text, or files.
///
/// It covers the whole page, so a drop lands also where a table has no rows.
final class DropView: NSView {
    /// Called with the text of what was dropped and, for a file, its name.
    var onDrop: ((String, URL?) -> Void)?
    private var targeted = false { didSet { needsDisplay = true } }

    override init(frame: NSRect) {
        super.init(frame: frame)
        registerForDraggedTypes([.fileURL, .string])
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    private func content(of info: NSDraggingInfo) -> (String, URL?)? {
        let board = info.draggingPasteboard
        if let url = (board.readObjects(forClasses: [NSURL.self], options: [.urlReadingFileURLsOnly: true]) as? [URL])?.first {
            // A profile or a rules file is small; anything large is not one.
            guard let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize, size <= 1 << 20,
                  let text = try? String(contentsOf: url, encoding: .utf8) else { return nil }
            return (text, url)
        }
        if let text = board.string(forType: .string), !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            return (text, nil)
        }
        return nil
    }

    override func draggingEntered(_ sender: NSDraggingInfo) -> NSDragOperation {
        targeted = true
        return .copy
    }

    override func draggingExited(_ sender: NSDraggingInfo?) { targeted = false }
    override func draggingEnded(_ sender: NSDraggingInfo) { targeted = false }

    override func performDragOperation(_ sender: NSDraggingInfo) -> Bool {
        targeted = false
        guard let (text, url) = content(of: sender) else { return false }
        onDrop?(text, url)
        return true
    }

    override func draw(_ dirtyRect: NSRect) {
        super.draw(dirtyRect)
        guard targeted else { return }
        let ring = NSBezierPath(roundedRect: bounds.insetBy(dx: 6, dy: 6), xRadius: 10, yRadius: 10)
        NSColor.controlAccentColor.withAlphaComponent(0.12).setFill()
        ring.fill()
        NSColor.controlAccentColor.setStroke()
        ring.lineWidth = 2
        ring.stroke()
    }
}

/// A table that hears the Delete key.
final class ListTableView: NSTableView {
    var onDelete: (() -> Void)?

    override func keyDown(with event: NSEvent) {
        // 51 is Delete, 117 is Forward Delete.
        if event.keyCode == 51 || event.keyCode == 117, selectedRow >= 0 {
            onDelete?()
        } else {
            super.keyDown(with: event)
        }
    }
}
