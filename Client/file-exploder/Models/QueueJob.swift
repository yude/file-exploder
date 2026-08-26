import Foundation

struct QueueJob: Identifiable, Codable {
    let id: String
    let type: OperationType
    let srcPath: String?
    let dstPath: String?
    let mode: String?
    var status: JobStatus // changed to var to allow updating
    var error: String?
    let createdAt: Date
    let startedAt: Date?
    let completedAt: Date?
    
    enum OperationType: String, Codable {
        case rename
        case move
        case delete
        case copy
        case mkdir
        case chmod
        
        var displayName: String {
            switch self {
            case .rename: return "名前変更"
            case .move: return "移動"
            case .delete: return "削除"
            case .copy: return "コピー"
            case .mkdir: return "新規フォルダ"
            case .chmod: return "権限変更"
            }
        }
        
        var systemImage: String {
            switch self {
            case .rename: return "pencil"
            case .move: return "arrow.right.doc.on.clipboard"
            case .delete: return "trash"
            case .copy: return "doc.on.doc"
            case .mkdir: return "folder.badge.plus"
            case .chmod: return "lock.shield"
            }
        }
    }
    
    enum JobStatus: String, Codable {
        case pending
        case running
        case completed
        case failed
        case cancelled
        
        var displayName: String {
            switch self {
            case .pending: return "待機中"
            case .running: return "実行中"
            case .completed: return "完了"
            case .failed: return "失敗"
            case .cancelled: return "キャンセル"
            }
        }
    }
    
    var description: String {
        switch type {
        case .rename:
            return "名前変更 \(srcPath ?? "") -> \(dstPath ?? "")"
        case .move:
            return "移動 \(srcPath ?? "") -> \(dstPath ?? "")"
        case .delete:
            return "削除 \(srcPath ?? "")"
        case .copy:
            return "コピー \(srcPath ?? "") -> \(dstPath ?? "")"
        case .mkdir:
            return "作成 \(dstPath ?? "")"
        case .chmod:
            return "権限変更 \(dstPath ?? "") to \(mode ?? "")"
        }
    }
}
