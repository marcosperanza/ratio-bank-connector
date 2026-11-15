// Regenerates the app icon (AppIcon.icns) and the menu bar images from the
// Ratio brand mark. Run: swift icons/make-icons.swift (from the macos dir).
import AppKit

let fm = FileManager.default
let dir = URL(fileURLWithPath: "icons")
let brand = NSImage(contentsOf: dir.appendingPathComponent("brand-1024.png"))!

func png(_ size: Int, draw: (CGContext, CGFloat) -> Void) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8,
                               samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                               bytesPerRow: 0, bitsPerPixel: 0)!
    let ctx = NSGraphicsContext(bitmapImageRep: rep)!
    NSGraphicsContext.current = ctx
    draw(ctx.cgContext, CGFloat(size))
    NSGraphicsContext.current = nil
    return rep.representation(using: .png, properties: [:])!
}

// macOS icon grid: the artwork is an 824/1024 rounded square, centred.
func appIcon(_ cg: CGContext, _ s: CGFloat) {
    let side = s * 824 / 1024
    let rect = CGRect(x: (s - side) / 2, y: (s - side) / 2, width: side, height: side)
    let path = CGPath(roundedRect: rect, cornerWidth: side * 0.225, cornerHeight: side * 0.225, transform: nil)
    cg.saveGState()
    cg.setShadow(offset: CGSize(width: 0, height: -s * 0.01), blur: s * 0.02, color: NSColor.black.withAlphaComponent(0.35).cgColor)
    cg.addPath(path)
    cg.setFillColor(NSColor(red: 0.14, green: 0.10, blue: 0.22, alpha: 1).cgColor)
    cg.fillPath()
    cg.restoreGState()
    cg.addPath(path)
    cg.clip()
    brand.draw(in: rect)
}

// The two chevrons of the web logo, in its 96x96 space with the 8/.8333 transform applied.
let upper = [(14, 16), (58, 16), (83, 41), (65, 59), (44, 59), (61, 42), (49, 30), (28, 30)]
let lower = [(82, 80), (38, 80), (13, 55), (31, 37), (52, 37), (35, 54), (47, 66), (68, 66)]

func mark(_ cg: CGContext, _ s: CGFloat, badge: Bool) {
    // Logo bounds are x 13...83, y 16...80; centre it.
    let k = s / 96 * 1.12, ox = s / 2 - 48 * k, oy = s / 2 - 48 * k
    func pt(_ p: (Int, Int)) -> CGPoint {
        let x = 8 + Double(p.0) * 0.833333, y = 8 + Double(p.1) * 0.833333
        return CGPoint(x: ox + x * k, y: s - (oy + y * k))
    }
    cg.setFillColor(NSColor.black.cgColor)
    for poly in [upper, lower] {
        cg.beginPath()
        cg.move(to: pt(poly[0]))
        for p in poly.dropFirst() { cg.addLine(to: pt(p)) }
        cg.closePath()
        cg.fillPath()
    }
    if badge {
        let r = s * 0.2
        let c = CGPoint(x: s - r - s * 0.02, y: r + s * 0.02)
        cg.setBlendMode(.clear)
        cg.fillEllipse(in: CGRect(x: c.x - r * 1.5, y: c.y - r * 1.5, width: r * 3, height: r * 3))
        cg.setBlendMode(.normal)
        cg.fillEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: r * 2, height: r * 2))
    }
}

// Menu bar images: 18pt at @1x/@2x, template (alpha only).
for (name, badge) in [("MenuBarIcon", false), ("MenuBarIconAlert", true)] {
    try png(18) { mark($0, $1, badge: badge) }.write(to: dir.appendingPathComponent("\(name).png"))
    try png(36) { mark($0, $1, badge: badge) }.write(to: dir.appendingPathComponent("\(name)@2x.png"))
}

let set = dir.appendingPathComponent("AppIcon.iconset")
try? fm.removeItem(at: set)
try fm.createDirectory(at: set, withIntermediateDirectories: true)
for base in [16, 32, 128, 256, 512] {
    try png(base, draw: appIcon).write(to: set.appendingPathComponent("icon_\(base)x\(base).png"))
    try png(base * 2, draw: appIcon).write(to: set.appendingPathComponent("icon_\(base)x\(base)@2x.png"))
}
