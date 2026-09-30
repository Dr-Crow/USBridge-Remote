// usbridge-ocr-helper: reads a PNG path from argv[1], runs it through
// Vision's text recognizer (the same OCR engine behind Preview/Photos'
// built-in "copy text from image" / Live Text), and prints the recognized
// lines to stdout, one per line, in reading order. Used by the Control
// footer's "Copy Text from Screen" tool (see screenshot_tool_darwin.go) so
// a Go binary doesn't need to link Vision.framework itself -- built once at
// app-build time (build_macos.sh) into Contents/MacOS/ rather than JIT-run
// via `swift <file>` each call: that path alone costs ~10s per invocation
// (measured live), against ~1-2s for this precompiled binary, almost all
// of which is Vision's own model load.
import Vision
import AppKit
import Foundation

guard CommandLine.arguments.count > 1 else {
    FileHandle.standardError.write("usage: usbridge-ocr-helper <image-path>\n".data(using: .utf8)!)
    exit(1)
}
let path = CommandLine.arguments[1]
guard let img = NSImage(contentsOfFile: path),
      let cgImage = img.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    FileHandle.standardError.write("usbridge-ocr-helper: failed to load image at \(path)\n".data(using: .utf8)!)
    exit(1)
}

let semaphore = DispatchSemaphore(value: 0)
var outputLines: [String] = []
var visionError: Error?

let request = VNRecognizeTextRequest { req, err in
    defer { semaphore.signal() }
    if let err = err {
        visionError = err
        return
    }
    guard let observations = req.results as? [VNRecognizedTextObservation] else {
        return
    }
    for obs in observations {
        if let top = obs.topCandidates(1).first {
            outputLines.append(top.string)
        }
    }
}
request.recognitionLevel = .accurate
request.usesLanguageCorrection = true
// Empty (not a fixed list) so Vision auto-detects the dominant script per
// run instead of only ever trying English -- this tool runs against
// whatever's on screen, which is as likely to be Cyrillic/CJK/etc. as
// English text.
request.recognitionLanguages = []

let handler = VNImageRequestHandler(cgImage: cgImage, options: [:])
do {
    try handler.perform([request])
} catch {
    FileHandle.standardError.write("usbridge-ocr-helper: \(error)\n".data(using: .utf8)!)
    exit(1)
}
semaphore.wait()

if let visionError = visionError {
    FileHandle.standardError.write("usbridge-ocr-helper: \(visionError)\n".data(using: .utf8)!)
    exit(1)
}

print(outputLines.joined(separator: "\n"))
