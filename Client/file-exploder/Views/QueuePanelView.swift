import SwiftUI

struct QueuePanelView: View {
    @State private var activeJobs: [QueueJob] = []
    @State private var logJobs: [QueueJob] = []
    @State private var isLoading = false
    @State private var errorMessage: String?
    @State private var selectedTab = 0
    
    let sftp: SFTPService?
    
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Header
            HStack {
                Text("ジョブ")
                    .font(.headline)
                Spacer()
                Button(action: { Task { await refresh() } }) {
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
            .onChange(of: selectedTab) {
                Task { await refresh() }
            }
            
            Divider()
            
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
                    })
                }
                .listStyle(.plain)
            }
        }
        .frame(width: 250)
        .task {
            await refresh()
        }
    }
    
    func refresh() async {
        guard let sftp = sftp else { return }
        isLoading = true
        errorMessage = nil
        
        do {
            if selectedTab == 0 {
                activeJobs = try await sftp.getQueueStatus()
            } else {
                logJobs = try await sftp.getJobLogs(limit: 50)
            }
        } catch {
            errorMessage = error.localizedDescription
        }
        
        isLoading = false
    }
}

struct QueueJobRow: View {
    let job: QueueJob
    let sftp: SFTPService?
    let onRefresh: () -> Void
    
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
                            try? await sftp.cancelJob(id: job.id)
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
