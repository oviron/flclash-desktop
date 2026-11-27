import AppKit

/// A policy opened by the engine for reading or editing.
struct PolicyDocument: Decodable {
    var name = ""
    var builtin = false
    var text = ""
    /// The rules read as three lists, and the lists say the same as the text.
    var listed = false
    var direct = ""
    var proxy = ""
    var block = ""
    var rest = "PROXY"
}

/// The sheet a policy is written in: three lists for most people, the rules
/// themselves for those who want the order in their own hands.
final class PolicyEditorController: NSViewController {
    /// The policy being edited; nil for a new one.
    private let original: String?
    private var document: PolicyDocument
    private let done: () -> Void

    private let name = NSTextField()
    private let tabs = NSSegmentedControl(labels: [L("Lists"), L("Rules")], trackingMode: .selectOne, target: nil, action: nil)
    private let direct = EditorField()
    private let proxy = EditorField()
    private let block = EditorField()
    private let rules = EditorField()
    private let rest = NSPopUpButton(frame: .zero, pullsDown: false)
    private let lists = NSStackView()
    private let problem = NSTextField(wrappingLabelWithString: "")
    private let save = NSButton(title: L("Save"), target: nil, action: nil)

    /// - Parameters:
    ///   - document: what to start from: an empty one, or one read by the engine.
    ///   - original: the name of the policy being edited; nil makes a new one.
    init(document: PolicyDocument, original: String?, done: @escaping () -> Void) {
        self.document = document
        self.original = original
        self.done = done
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    override func loadView() {
        let title = NSTextField(labelWithString: original == nil ? L("New Policy") : String(format: L("Policy “%@”"), document.name))
        title.font = .systemFont(ofSize: 15, weight: .semibold)

        name.placeholderString = L("Name: letters, digits and dashes")
        name.stringValue = document.name
        name.isHidden = original != nil

        tabs.target = self
        tabs.action = #selector(tabChanged)
        tabs.selectedSegment = document.listed ? 0 : 1
        // Rules that are not in the order of lists cannot be shown as lists
        // without changing what they mean.
        tabs.setEnabled(document.listed, forSegment: 0)

        direct.text = document.direct
        proxy.text = document.proxy
        block.text = document.block
        rules.text = document.text

        rest.addItems(withTitles: [L("through the server"), L("directly")])
        rest.selectItem(at: document.rest == "DIRECT" ? 1 : 0)
        let restLabel = NSTextField(labelWithString: L("Everything else goes"))
        let restRow = NSStackView(views: [restLabel, rest])
        restRow.orientation = .horizontal
        restRow.spacing = 8

        let hint = NSTextField(wrappingLabelWithString: L("One per line. A name stands for itself and everything under it: example.com, .ru. A network: 10.0.0.0/8. Or say the kind: domain:exact.example, keyword:word, geosite:youtube, geoip:us."))
        hint.font = .systemFont(ofSize: 11)
        hint.textColor = .secondaryLabelColor
        hint.preferredMaxLayoutWidth = 520

        lists.orientation = .vertical
        lists.alignment = .leading
        lists.spacing = 6
        for (label, field, tint, symbol) in [
            (L("Directly"), direct, NSColor.systemTeal, "arrow.right"),
            (L("Through the server"), proxy, NSColor.systemIndigo, "shield.lefthalf.filled"),
            (L("Refuse"), block, NSColor.systemRed, "nosign"),
        ] {
            let heading = NSStackView(views: [IconTile(symbol: symbol, tint: tint, side: 18), NSTextField(labelWithString: label)])
            heading.orientation = .horizontal
            heading.spacing = 6
            lists.addArrangedSubview(heading)
            lists.addArrangedSubview(field)
            field.heightAnchor.constraint(equalToConstant: 74).isActive = true
            field.widthAnchor.constraint(equalTo: lists.widthAnchor).isActive = true
            lists.setCustomSpacing(12, after: field)
        }
        lists.addArrangedSubview(restRow)
        lists.setCustomSpacing(10, after: restRow)
        lists.addArrangedSubview(hint)

        rules.heightAnchor.constraint(greaterThanOrEqualToConstant: 330).isActive = true

        problem.textColor = .systemRed
        problem.font = .systemFont(ofSize: 11)
        problem.preferredMaxLayoutWidth = 520
        problem.isHidden = true

        let cancel = NSButton(title: L("Cancel"), target: self, action: #selector(cancelClicked))
        cancel.keyEquivalent = "\u{1b}"
        save.target = self
        save.action = #selector(saveClicked)
        save.keyEquivalent = "\r"
        let gap = NSView()
        gap.setContentHuggingPriority(.init(1), for: .horizontal)
        let buttons = NSStackView(views: [gap, cancel, save])
        buttons.orientation = .horizontal
        buttons.spacing = 8

        let head = NSStackView(views: [title, NSView(), tabs])
        head.orientation = .horizontal
        head.alignment = .centerY

        let column = NSStackView(views: [head, name, lists, rules, problem, buttons])
        column.orientation = .vertical
        column.alignment = .leading
        column.spacing = 12
        column.translatesAutoresizingMaskIntoConstraints = false
        let root = NSView()
        root.addSubview(column)
        NSLayoutConstraint.activate([
            root.widthAnchor.constraint(equalToConstant: 560),
            column.topAnchor.constraint(equalTo: root.topAnchor, constant: 20),
            column.leadingAnchor.constraint(equalTo: root.leadingAnchor, constant: 20),
            column.trailingAnchor.constraint(equalTo: root.trailingAnchor, constant: -20),
            column.bottomAnchor.constraint(equalTo: root.bottomAnchor, constant: -16),
        ])
        for view in [head, name, lists, rules, buttons] as [NSView] {
            view.widthAnchor.constraint(equalTo: column.widthAnchor).isActive = true
        }
        view = root
        tabChanged()
    }

    @objc private func tabChanged() {
        let asLists = tabs.selectedSegment == 0
        lists.isHidden = !asLists
        rules.isHidden = asLists
        problem.isHidden = true
    }

    @objc private func cancelClicked() {
        dismiss(nil)
    }

    @objc private func saveClicked() {
        // The file name of a policy is its name, in the form the engine takes.
        let raw = (original ?? name.stringValue).trimmingCharacters(in: .whitespaces).lowercased()
        let slug = String(raw.map { $0.isASCII && ($0.isLetter || $0.isNumber || $0 == "-" || $0 == "_") ? $0 : "-" })
        guard !slug.isEmpty else {
            show(L("Give the policy a name."))
            return
        }
        var request = ["name": slug]
        if tabs.selectedSegment == 0 {
            request["op"] = "policyWriteLists"
            request["direct"] = direct.text
            request["proxy"] = proxy.text
            request["block"] = block.text
            request["rest"] = rest.indexOfSelectedItem == 1 ? "DIRECT" : "PROXY"
        } else {
            request["op"] = "policyWrite"
            request["text"] = rules.text
        }
        save.isEnabled = false
        Model.shared.writePolicy(request) { [weak self] error in
            guard let self else { return }
            self.save.isEnabled = true
            if let error {
                self.show(error)
                return
            }
            self.done()
            self.dismiss(nil)
        }
    }

    private func show(_ text: String) {
        problem.stringValue = text
        problem.isHidden = false
    }
}

/// A plain text box for lines of names and rules: fixed pitch, and none of
/// the help a text field gives to prose.
final class EditorField: NSScrollView {
    private let field = NSTextView()

    var text: String {
        get { field.string }
        set { field.string = newValue }
    }

    init() {
        super.init(frame: .zero)
        field.isRichText = false
        field.font = .monospacedSystemFont(ofSize: 12, weight: .regular)
        field.isAutomaticQuoteSubstitutionEnabled = false
        field.isAutomaticDashSubstitutionEnabled = false
        field.isAutomaticTextReplacementEnabled = false
        field.isAutomaticSpellingCorrectionEnabled = false
        field.isContinuousSpellCheckingEnabled = false
        field.isGrammarCheckingEnabled = false
        field.allowsUndo = true
        field.textContainerInset = NSSize(width: 4, height: 6)
        field.isVerticallyResizable = true
        field.autoresizingMask = [.width]
        field.textContainer?.widthTracksTextView = true
        documentView = field
        hasVerticalScroller = true
        borderType = .bezelBorder
        translatesAutoresizingMaskIntoConstraints = false
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }
}
