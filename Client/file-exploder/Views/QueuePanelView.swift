import SwiftUI
#if os(macOS)
import AppKit
#endif

struct QueuePanelView: View {
    @State private var activeJobs: [QueueJob] = []
    @State private var logJobs: [QueueJob] = []
    @State private var isLoading = false
    @State private var isRefreshing = false
    @State private var errorMessage: String?
    /// Kept apart from errorMessage: a failed action is the user's own doing and
    /// must survive the next poll, which clears the polling error every two
    /// seconds. It also shows above the list rather than replacing it.
    @State private var actionError: String?
    @State private var selectedTab = 0
    
    let sftp: SFTPService?
    
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text("ジョブ")
                    .font(.headline)
                Spacer()
                if selectedTab == 1 && !logJobs.isEmpty {
                    Button(action: copyAllLogs) {
                        Image(systemName: "doc.on.doc")
                    }
                    .buttonStyle(.borderless)
                    .help("履歴をすべてコピー")
                    .accessibilityLabel("履歴をすべてコピー")
                }
                Button(action: {
                    actionError = nil
                    Task { await refresh() }
                }) {
                    Image(systemName: "arrow.clockwise")
                }
                .buttonStyle(.borderless)
            }
            .padding(.horizontal)
            .padding(.vertical, 8)
            
            Picker("", selection: $selectedTab) {
                Text("キュー").tag(0)
                Text("履歴").tag(1)
            }
            .pickerStyle(.segmented)
            .padding(.horizontal)
            .padding(.bottom, 8)
            // The polling task no longer restarts on a tab switch (it's keyed
            // on the connection alone now, so it stops wiping the other tab's
            // already-loaded data) - but that means switching tabs no longer
            // forces an immediate fetch either, so the just-selected tab
            // would otherwise show stale or empty data until the next
            // 2-second tick happens to land. refresh() already guards
            // against overlapping an in-flight poll.
            .onChange(of: selectedTab) { _, _ in
                Task { await refresh() }
            }
            
            Divider()

            if let actionError {
                HStack(alignment: .top, spacing: 6) {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundColor(.orange)
                    Text(actionError)
                        .font(.caption2)
                        .foregroundColor(.secondary)
                    Spacer(minLength: 0)
                    Button(action: { self.actionError = nil }) {
                        Image(systemName: "xmark")
                    }
                    .buttonStyle(.borderless)
                }
                .padding(.horizontal)
                .padding(.vertical, 6)
                Divider()
            }

            let displayJobs = selectedTab == 0 ? activeJobs.filter { $0.status == .pending || $0.status == .running } : logJobs
            
            if isLoading {
                Spacer()
                ProgressView()
                    .frame(maxWidth: .infinity)
                Spacer()
            } else if let error = errorMessage {
                Spacer()
                Text(error)
                    .foregroundColor(.red)
                    .font(.caption)
                    .padding()
                Spacer()
            } else if displayJobs.isEmpty {
                Spacer()
                VStack(spacing: 8) {
                    Image(systemName: selectedTab == 0 ? "tray" : "clock")
                        .font(.title2)
                        .foregroundColor(.secondary)
                    Text(selectedTab == 0 ? "待機中のジョブはありません" : "履歴がありません")
                        .foregroundColor(.secondary)
                        .font(.caption)
                }
                .frame(maxWidth: .infinity)
                Spacer()
            } else {
                List(displayJobs) { job in
                    QueueJobRow(job: job, sftp: sftp, onRefresh: {
                        Task { await refresh() }
                    }, onError: { message in
                        actionError = message
                    })
                    .contextMenu {
                        if selectedTab == 1 {
                            Button("ログをコピー") {
                                copyToPasteboard(job.clipboardLog)
                            }
                        }
                    }
                }
                .listStyle(.plain)
            }
        }
        .frame(width: 250)
        // Keyed on the connection alone, not the selected tab: this task used
        // to restart - wiping both tabs' already-loaded jobs and forcing a
        // fresh fetch - on every switch between "キュー" and "履歴", even
        // though nothing about the connection changed. refresh() already
        // reads selectedTab itself on every tick, so the same long-lived loop
        // keeps whichever tab is on screen up to date without touching the
        // other tab's data.
        .task(id: sftp.map { ObjectIdentifier($0) }) {
            // The task restarts when the connection changes, so this is also
            // where the previous session's jobs have to go: leaving them meant
            // the panel kept listing a disconnected server's queue as if it
            // were live.
            activeJobs = []
            logJobs = []
            errorMessage = nil

            // Without a connection there is nothing to poll; looping anyway
            // just wakes the panel every two seconds to do nothing.
            guard sftp != nil else {
                isLoading = false
                return
            }
            isLoading = true
            while !Task.isCancelled {
                await refresh()
                do {
                    try await Task.sleep(nanoseconds: 2_000_000_000)
                } catch {
                    break
                }
            }
        }
    }

    private func copyAllLogs() {
        copyToPasteboard(logJobs.map(\.clipboardLog).joined(separator: "\n\n---\n\n"))
    }

    private func copyToPasteboard(_ text: String) {
        #if os(macOS)
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(text, forType: .string)
        #endif
    }
    
    func refresh() async {
        guard let sftp = sftp else {
            isLoading = false
            return
        }
        guard !isRefreshing else { return }
        isRefreshing = true
        errorMessage = nil
        defer {
            isRefreshing = false
            isLoading = false
        }
        
        do {
            if selectedTab == 0 {
                activeJobs = try await sftp.getQueueStatus()
            } else {
                logJobs = try await sftp.getJobLogs(limit: 50)
            }
        } catch {
            if error is CancellationError { return }
            errorMessage = error.localizedDescription
        }
    }
}

struct QueueJobRow: View {
    let job: QueueJob
    let sftp: SFTPService?
    let onRefresh: () -> Void
    let onError: (String) -> Void
    
    var statusColor: Color {
        switch job.status {
        case .pending: return .orange
        case .running: return .blue
        case .completed: return .green
        case .failed: return .red
        case .cancelled: return .gray
        }
    }
    
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack {
                Image(systemName: job.type.systemImage)
                    .foregroundColor(statusColor)
                
                Text(job.type.displayName)
                    .font(.caption)
                    .fontWeight(.medium)
                
                Spacer()
                
                Text(job.status.displayName)
                    .font(.caption2)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(statusColor.opacity(0.2))
                    .foregroundColor(statusColor)
                    .cornerRadius(4)
            }
            
            Text(job.description)
                .font(.caption2)
                .foregroundColor(.secondary)
                .lineLimit(2)
            
            if let error = job.error {
                Text(error)
                    .font(.caption2)
                    .foregroundColor(.red)
                    .lineLimit(2)
            }
            
            if job.status == .completed || job.status == .failed || job.status == .cancelled, let date = job.completedAt {
                Text(FormatUtils.formattedDate(date))
                    .font(.system(size: 9))
                    .foregroundColor(.secondary)
            }
            
            if job.status == .pending {
                Button("キャンセル") {
                    Task {
                        if let sftp = sftp {
                            do {
                                try await sftp.cancelJob(id: job.id)
                            } catch {
                                onError("キャンセルに失敗しました: \(error.localizedDescription)")
                                return
                            }
                            onRefresh()
                        }
                    }
                }
                .buttonStyle(.borderless)
                .font(.caption2)
            }
        }
        .padding(.vertical, 4)
    }
}

#Preview {
    QueuePanelView(sftp: nil)
}
