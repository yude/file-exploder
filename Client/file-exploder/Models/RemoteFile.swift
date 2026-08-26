import Foundation

struct RemoteFile: Identifiable, Hashable {
    var id: String { path }
    let name: String
    let path: String
    let size: Int64
    let modificationDate: Date
    var isDirectory: Bool // Changed to var so permissions can mutate it
    var permissions: FilePermissions {
        didSet {
            permissions.isDirectory = self.isDirectory
        }
    }
    
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
    var isDirectory: Bool = false
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
        var str = isDirectory ? "d" : "-"
        
        str += ownerRead ? "r" : "-"
        str += ownerWrite ? "w" : "-"
        str += ownerExecute ? "x" : "-"
        str += groupRead ? "r" : "-"
        str += groupWrite ? "w" : "-"
        str += groupExecute ? "x" : "-"
        str += otherRead ? "r" : "-"
        str += otherWrite ? "w" : "-"
        str += otherExecute ? "x" : "-"
        return str
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
