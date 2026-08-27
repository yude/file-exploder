import Foundation

struct QueueJob: Identifiable, Codable {
    let id: String
    let type: OperationType
    let srcPath: String?
    let dstPath: String?
    let mode: String?
    let status: JobStatus
    let error: String?
    let createdAt: Date
    let startedAt: Date?
    let completedAt: Date?

    /// The daemon serialises jobs with snake_case keys (see `queue.Job` in the
    /// Go server), so the mapping has to be spelled out.
    enum CodingKeys: String, CodingKey {
        case id
        case type
        case srcPath = "src_path"
        case dstPath = "dst_path"
        case mode
        case status
        case error
        case createdAt = "created_at"
        case startedAt = "started_at"
        case completedAt = "completed_at"
    }

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

    /// A stable, self-contained representation suitable for pasting into an
    /// issue or a support message. Unlike the compact row, this deliberately
    /// includes the job ID and every available timestamp.
    var clipboardLog: String {
        var lines = [
            "ID: \(id)",
            "操作: \(type.displayName)",
            "状態: \(status.displayName)",
            "内容: \(description)",
            "作成日時: \(Self.logDateFormatter.string(from: createdAt))"
        ]

        if let startedAt {
            lines.append("開始日時: \(Self.logDateFormatter.string(from: startedAt))")
        }
        if let completedAt {
            lines.append("完了日時: \(Self.logDateFormatter.string(from: completedAt))")
        }
        if let error, !error.isEmpty {
            lines.append("エラー: \(error)")
        }

        return lines.joined(separator: "\n")
    }

    private static let logDateFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        return formatter
    }()
}
