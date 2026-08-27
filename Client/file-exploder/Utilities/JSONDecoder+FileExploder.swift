import Foundation

extension JSONDecoder {
    static func fileExploderDecoder() -> JSONDecoder {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let container = try decoder.singleValueContainer()
            let value = try container.decode(String.self)

            guard let date = RFC3339Parser.date(from: value) else {
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription: "Invalid ISO 8601 date: \(value)"
                )
            }
            return date
        }
        return decoder
    }
}

/// Parses the RFC 3339 timestamps emitted by the Go daemon.
///
/// Go's `time.Time` marshals with up to nine fractional digits (and trims
/// trailing zeros), while `ISO8601DateFormatter` only reliably accepts three.
/// Values that the formatter rejects are retried with the fraction normalised
/// to three digits so job timestamps never fail to decode.
enum RFC3339Parser {
    private static let fractional: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()

    private static let plain: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    static func date(from value: String) -> Date? {
        if let date = fractional.date(from: value) ?? plain.date(from: value) {
            return date
        }
        guard let normalized = normalizingFractionalSeconds(value) else { return nil }
        return fractional.date(from: normalized) ?? plain.date(from: normalized)
    }

    /// Rewrites `12:00:05.123456789Z` as `12:00:05.123Z`. Returns nil when the
    /// value carries no fractional seconds, since there is nothing to retry.
    private static func normalizingFractionalSeconds(_ value: String) -> String? {
        guard let dotIndex = value.lastIndex(of: ".") else { return nil }
        let afterDot = value.index(after: dotIndex)
        guard let suffixStart = value[afterDot...].firstIndex(where: { !$0.isNumber }) else { return nil }
        let digits = value[afterDot..<suffixStart]
        guard !digits.isEmpty else { return nil }

        var fraction = String(digits.prefix(3))
        fraction += String(repeating: "0", count: 3 - fraction.count)
        return String(value[..<afterDot]) + fraction + String(value[suffixStart...])
    }
}
