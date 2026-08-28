import Foundation

/// Decodes one element of an array that might not match `Wrapped` - an
/// unrecognized enum raw value from an older or newer version of this app or
/// the server, say - without failing the whole array. Use as
/// `[FailableDecodable<T>].self`, then `.compactMap(\.value)` to drop
/// whichever elements didn't decode instead of losing every element to one
/// bad entry.
struct FailableDecodable<Wrapped: Decodable>: Decodable {
    let value: Wrapped?

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        value = try? container.decode(Wrapped.self)
    }
}
