import AppKit

final class MainWindowController: NSWindowController {
    init() {
        let window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 860, height: 540),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered, defer: false
        )
        window.title = "FlClash"
        window.titlebarAppearsTransparent = false
        window.toolbarStyle = .unified
        window.minSize = NSSize(width: 640, height: 400)
        window.isReleasedWhenClosed = false
        window.setFrameAutosaveName("FlClashMainWindow")
        window.contentViewController = RootSplitViewController()
        if window.frame.width < 640 { window.setContentSize(NSSize(width: 860, height: 540)) }
        window.center()
        _ = window.setFrameUsingName("FlClashMainWindow")
        super.init(window: window)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }
}

/// A sidebar with the two things the app has, and the chosen one beside it.
final class RootSplitViewController: NSSplitViewController {
    private let sidebar = SidebarViewController()
    private let content = ContentViewController()

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
        sidebar.onSelect = { [weak self] page in self?.content.show(page) }
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
    private var pages: [Page: NSViewController] = [:]
    private var current: NSViewController?

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
        let next = pages[page] ?? {
            let made: NSViewController = page == .servers ? ServersViewController() : RoutingViewController()
            pages[page] = made
            return made
        }()
        guard next !== current else { return }
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
        view.window?.makeFirstResponder(next.view)
    }
}
