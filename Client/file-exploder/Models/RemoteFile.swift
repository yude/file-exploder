import Foundation

struct RemoteFile: Identifiable, Hashable {
    let id = UUID()
    let name: String
    let path: String
    let size: Int64
    let modificationDate: Date
    let isDirectory: Bool
    let permissions: FilePermissions
    
    var displayName: String {
        name
    }
    
    var formattedSize: String {
        if isDirectory {
            return "--"
        }
        return ByteCountFormatter.string(fromByteCount: size, countStyle: .file)
    }
    
    var formattedDate: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: modificationDate)
    }
    
    func hash(into hasher: inout Hasher) {
        hasher.combine(id)
    }
    
    static func == (lhs: RemoteFile, rhs: RemoteFile) -> Bool {
        lhs.id == rhs.id
    }
}

struct FilePermissions: Hashable {
    let ownerRead: Bool
    let ownerWrite: Bool
    let ownerExecute: Bool
    let groupRead: Bool
    let groupWrite: Bool
    let groupExecute: Bool
    let otherRead: Bool
    let otherWrite: Bool
    let otherExecute: Bool
    
    var octalString: String {
        let owner = (ownerRead ? 4 : 0) + (ownerWrite ? 2 : 0) + (ownerExecute ? 1 : 0)
        let group = (groupRead ? 4 : 0) + (groupWrite ? 2 : 0) + (groupExecute ? 1 : 0)
        let other = (otherRead ? 4 : 0) + (otherWrite ? 2 : 0) + (otherExecute ? 1 : 0)
        return "\(owner)\(group)\(other)"
    }
    
    var symbolicString: String {
        func triad(_ r: Bool, _ w: Bool, _ x: Bool) -> String {
            (r ? "r" : "-") + (w ? "w" : "-") + (x ? "x" : "-")
        }
        return "d" + triad(ownerRead, ownerWrite, ownerExecute)
            + triad(groupRead, groupWrite, groupExecute)
            + triad(otherRead, otherWrite, otherExecute)
    }
    
    static func from(octal: Int) -> FilePermissions {
        FilePermissions(
            ownerRead: octal & 0o400 != 0,
            ownerWrite: octal & 0o200 != 0,
            ownerExecute: octal & 0o100 != 0,
            groupRead: octal & 0o040 != 0,
            groupWrite: octal & 0o020 != 0,
            groupExecute: octal & 0o010 != 0,
            otherRead: octal & 0o004 != 0,
            otherWrite: octal & 0o002 != 0,
            otherExecute: octal & 0o001 != 0
        )
    }
}
