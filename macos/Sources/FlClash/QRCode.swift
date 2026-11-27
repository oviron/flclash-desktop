import AppKit
import CoreImage
import Vision

enum QRCode {
    /// Draws text as a QR code, sharp at any size.
    static func image(of text: String, side: CGFloat) -> NSImage? {
        guard let filter = CIFilter(name: "CIQRCodeGenerator") else { return nil }
        filter.setValue(Data(text.utf8), forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")
        guard let output = filter.outputImage, output.extent.width > 0 else { return nil }
        let scale = max(1, (side * 2 / output.extent.width).rounded(.down))
        let scaled = output.transformed(by: CGAffineTransform(scaleX: scale, y: scale))
        let rep = NSCIImageRep(ciImage: scaled)
        let image = NSImage(size: NSSize(width: side, height: side))
        image.addRepresentation(rep)
        return image
    }

    /// Reads the QR codes in a picture: a screenshot, a photo, a saved code.
    static func texts(in image: NSImage) -> [String] {
        guard let cg = image.cgImage(forProposedRect: nil, context: nil, hints: nil) else { return [] }
        let request = VNDetectBarcodesRequest()
        request.symbologies = [.qr]
        try? VNImageRequestHandler(cgImage: cg).perform([request])
        return (request.results ?? []).compactMap(\.payloadStringValue)
    }
}

/// The sheet that shows a server as a QR code, for another device to scan.
final class QRSheetController: NSViewController {
    private let name: String
    private let link: String

    init(name: String, link: String) {
        self.name = name
        self.link = link
        super.init(nibName: nil, bundle: nil)
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    override func loadView() {
        let title = NSTextField(labelWithString: name)
        title.font = .systemFont(ofSize: 15, weight: .semibold)
        let picture = NSImageView()
        picture.image = QRCode.image(of: link, side: 280)
        picture.imageScaling = .scaleProportionallyUpOrDown
        // A code is read against white, whatever the theme.
        let plate = NSBox()
        plate.boxType = .custom
        plate.titlePosition = .noTitle
        plate.borderWidth = 0
        plate.cornerRadius = 12
        plate.fillColor = .white
        plate.contentViewMargins = NSSize(width: 14, height: 14)
        plate.contentView = picture
        let note = NSTextField(wrappingLabelWithString: L("The code holds the keys of this server. Show it only to devices you trust."))
        note.font = .systemFont(ofSize: 11)
        note.textColor = .secondaryLabelColor
        note.alignment = .center
        note.preferredMaxLayoutWidth = 300
        let copy = NSButton(title: L("Copy Link"), target: self, action: #selector(copyLink))
        let close = NSButton(title: L("Done"), target: self, action: #selector(done))
        close.keyEquivalent = "\r"
        let buttons = NSStackView(views: [copy, close])
        buttons.spacing = 8
        let column = NSStackView(views: [title, plate, note, buttons])
        column.orientation = .vertical
        column.alignment = .centerX
        column.spacing = 14
        column.edgeInsets = NSEdgeInsets(top: 20, left: 24, bottom: 18, right: 24)
        NSLayoutConstraint.activate([
            picture.widthAnchor.constraint(equalToConstant: 280),
            picture.heightAnchor.constraint(equalToConstant: 280),
        ])
        view = column
    }

    @objc private func copyLink() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(link, forType: .string)
    }

    @objc private func done() { dismiss(nil) }
}
