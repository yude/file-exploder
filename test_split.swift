import Foundation
let line = "drwxr-xr-x   2 user     group        4096 Aug 17 10:00 dir name"
let parts = line.split(separator: " ", maxSplits: 8)
for (i, p) in parts.enumerated() {
    print("\(i): '\(p)'")
}
