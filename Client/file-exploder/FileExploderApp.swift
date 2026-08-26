import SwiftUI
#if os(macOS)
import AppKit

class AppDelegate: NSObject, NSApplicationDelegate {
    func applicationWillFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
    }
    
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.activate(ignoringOtherApps: true)
    }
}
#endif

@main
struct FileExploderApp: App {
    
    #if os(macOS)
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    #endif

    var body: some Scene {
        WindowGroup {
            MainView()
        }
        .commands {
            CommandGroup(replacing: .newItem) {
                Button("新しいウィンドウ") {
                    // Open new window
                }
                .keyboardShortcut("n", modifiers: .command)
            }
        }
        
        #if os(macOS)
        Settings {
            SettingsView()
        }
        #endif
    }
}

struct SettingsView: View {
    @AppStorage("showHiddenFiles") private var showHiddenFiles = false
    @AppStorage("refreshInterval") private var refreshInterval = 5.0
    
    var body: some View {
        Form {
            Section("一般") {
                Toggle("隠しファイルを表示", isOn: $showHiddenFiles)
                Slider(value: $refreshInterval, in: 1...30, step: 1) {
                    Text("更新間隔 (秒)")
                }
            }
        }
        .padding()
        .frame(width: 350)
    }
}
