import CoreText
import SwiftUI

// THE BRAND'S TWO TYPEFACES (skill tappa-brand; M16-15 hand-off 2): Space Grotesk for every word,
// IBM Plex Mono for every number. The app ships the SAME files the web serves -- the six woff2 of
// web/static/fonts/, copied into the bundle by the Xcode project from where they live, with their
// two OFL texts beside them (the licence travels with the fonts). Nothing is downloaded, at build
// time or at run time: there is one copy of the bytes in the repository, and its provenance, sizes
// and SHA-256 are in web/static/fonts/README.md.
//
// Core Text reads woff2 directly (CTFontManagerCreateFontDescriptorsFromData -- measured on the
// build Mac and in the simulator, TypefaceTests). The fonts are NOT registered with the system
// (no UIAppFonts, no CTFontManagerRegister...): the descriptors are used where they are made, so
// the two subsets of a face, which share one PostScript name, never collide.
//
// WHY A CASCADE. Each face comes as two subsets: `latin` and `latin-ext`, which carries the
// Maltese letters ċ ġ ħ ż. The web picks between them with unicode-range; here the latin subset
// is the font and the latin-ext subset of the SAME weight is its cascade list, so "Ċikku Żammit"
// is drawn in Space Grotesk rather than in a system fallback (TypefaceTests reads the runs).
//
// Space Grotesk is a variable font (wght 300-700); a weight is the wght axis set on its
// descriptor, the way the browser's font-weight picks it. If a file cannot be read the system
// face stands in -- a screen in the wrong typeface is a cosmetic fault, a screen that does not
// draw is not.
enum Typeface {
    enum Weight: Int {
        case regular = 400
        case medium = 500
        case bold = 700
    }

    static func display(_ size: CGFloat, _ weight: Weight = .regular) -> Font {
        guard let f = Faces.shared.display(size, weight) else {
            return .system(size: size, weight: weight == .bold ? .bold : (weight == .medium ? .medium : .regular))
        }
        return Font(f)
    }

    static func mono(_ size: CGFloat, bold: Bool = false) -> Font {
        guard let f = Faces.shared.mono(size, bold: bold) else {
            return .system(size: size, weight: bold ? .bold : .regular, design: .monospaced)
        }
        return Font(f)
    }

    /// The bundled files: published name -> file name in the app bundle.
    enum File {
        static let displayLatin = "space-grotesk-v22-latin-wght"
        static let displayExt = "space-grotesk-v22-latin-ext-wght"
        static let monoLatin400 = "ibm-plex-mono-v20-latin-400"
        static let monoExt400 = "ibm-plex-mono-v20-latin-ext-400"
        static let monoLatin700 = "ibm-plex-mono-v20-latin-700"
        static let monoExt700 = "ibm-plex-mono-v20-latin-ext-700"
        static let all = [displayLatin, displayExt, monoLatin400, monoExt400, monoLatin700, monoExt700]
    }

    /// The descriptors, read once from the bundle.
    final class Faces {
        static let shared = Faces(bundle: .main)

        private let displayLatin: CTFontDescriptor?
        private let displayExt: CTFontDescriptor?
        private let monoLatin400: CTFontDescriptor?
        private let monoExt400: CTFontDescriptor?
        private let monoLatin700: CTFontDescriptor?
        private let monoExt700: CTFontDescriptor?

        init(bundle: Bundle) {
            func first(_ name: String) -> CTFontDescriptor? {
                guard let url = bundle.url(forResource: name, withExtension: "woff2"),
                      let data = try? Data(contentsOf: url),
                      let list = CTFontManagerCreateFontDescriptorsFromData(data as CFData) as? [CTFontDescriptor] else {
                    return nil
                }
                return list.first
            }
            displayLatin = first(File.displayLatin)
            displayExt = first(File.displayExt)
            monoLatin400 = first(File.monoLatin400)
            monoExt400 = first(File.monoExt400)
            monoLatin700 = first(File.monoLatin700)
            monoExt700 = first(File.monoExt700)
        }

        /// The wght axis tag, 'wght'.
        private static let weightAxis = 0x7767_6874

        func display(_ size: CGFloat, _ weight: Weight) -> CTFont? {
            guard let latin = displayLatin else {
                return nil
            }
            let axis = [Faces.weightAxis: weight.rawValue] as CFDictionary
            var cascade: [CTFontDescriptor] = []
            if let ext = displayExt {
                cascade.append(CTFontDescriptorCreateCopyWithAttributes(ext, [kCTFontVariationAttribute: axis] as CFDictionary))
            }
            let d = CTFontDescriptorCreateCopyWithAttributes(latin, [
                kCTFontVariationAttribute: axis,
                kCTFontCascadeListAttribute: cascade,
            ] as CFDictionary)
            return CTFontCreateWithFontDescriptor(d, size, nil)
        }

        func mono(_ size: CGFloat, bold: Bool) -> CTFont? {
            guard let latin = bold ? monoLatin700 : monoLatin400 else {
                return nil
            }
            let cascade = [bold ? monoExt700 : monoExt400].compactMap { $0 }
            let d = CTFontDescriptorCreateCopyWithAttributes(latin, [kCTFontCascadeListAttribute: cascade] as CFDictionary)
            return CTFontCreateWithFontDescriptor(d, size, nil)
        }
    }
}
