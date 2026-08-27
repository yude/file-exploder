import Foundation

struct RemoteFile: Identifiable, Hashable {
    /// A byte-exact stand-in for `path`.
    ///
    /// Swift compares Strings by Unicode canonical equivalence, so an NFC and
    /// an NFD spelling of the same name are `==` and hash alike. A Linux server
    /// keeps them as two genuinely different files — a directory shared with a
    /// Mac routinely ends up holding both — and every identity check in the file
    /// list runs through this id: Table's row identity, the selection set, and
    /// `first(where: { $0.id == fileId })` behind the context menu. Sharing an
    /// id there means an action can land on the wrong file. Encoding the path's
    /// bytes keeps the id ASCII, so String equality is byte equality again.
    let id: String
    let name: String
    let path: String
    let size: Int64
    let modificationDate: Date
    let isDirectory: Bool
    let isSymlink: Bool
    let permissions: FilePermissions

    init(
        name: String,
        path: String,
        size: Int64,
        modificationDate: Date,
        isDirectory: Bool,
        isSymlink: Bool,
        permissions: FilePermissions
    ) {
        self.id = RemoteFile.identity(for: path)
        self.name = name
        self.path = path
        self.size = size
        self.modificationDate = modificationDate
        self.isDirectory = isDirectory
        self.isSymlink = isSymlink
        self.permissions = permissions
    }

    private static let hexDigits = Array("0123456789abcdef")

    private static func identity(for path: String) -> String {
        var characters: [Character] = []
        characters.reserveCapacity(path.utf8.count * 2)
        for byte in path.utf8 {
            characters.append(hexDigits[Int(byte >> 4)])
            characters.append(hexDigits[Int(byte & 0x0f)])
        }
        return String(characters)
    }

    var displayName: String {
        name
    }
    
    var formattedSize: String {
        if isDirectory {
            return "--"
        }
        return FormatUtils.formattedSize(size)
    }
    
    var formattedDate: String {
        FormatUtils.formattedDate(modificationDate)
    }

    var symbolicPermissions: String {
        permissions.symbolicString(isDirectory: isDirectory, isSymlink: isSymlink)
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
    
    func symbolicString(isDirectory: Bool, isSymlink: Bool) -> String {
        var str: String
        if isSymlink {
            str = "l"
        } else if isDirectory {
            str = "d"
        } else {
            str = "-"
        }
        
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
