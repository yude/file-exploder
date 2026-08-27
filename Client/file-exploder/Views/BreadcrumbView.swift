import SwiftUI

struct BreadcrumbView: View {
    let path: String
    let rootPath: String
    let onNavigate: (String) -> Void
    
    var pathComponents: [(String, String)] {
        var components: [(String, String)] = []
        let root = (rootPath as NSString).standardizingPath
        let current = (path as NSString).standardizingPath
        let relativePath: String
        if root == "/" {
            relativePath = String(current.dropFirst())
        } else if current == root {
            relativePath = ""
        } else {
            relativePath = String(current.dropFirst(root.count + 1))
        }
        let parts = relativePath.split(separator: "/").map(String.init)

        var currentPath = root
        for part in parts {
            currentPath = (currentPath as NSString).appendingPathComponent(part)
            components.append((part, currentPath))
        }
        
        return components
    }
    
    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 4) {
                // Root
                Button(action: {
                    onNavigate((rootPath as NSString).standardizingPath)
                }) {
                    Image(systemName: "house")
                }
                .buttonStyle(.borderless)
                
                ForEach(Array(pathComponents.enumerated()), id: \.offset) { index, component in
                    Image(systemName: "chevron.right")
                        .font(.caption2)
                        .foregroundColor(.secondary)
                    
                    Button(action: { onNavigate(component.1) }) {
                        Text(component.0)
                    }
                    .buttonStyle(.borderless)
                }
            }
            .font(.callout)
        }
    }
}

#Preview {
    BreadcrumbView(path: "/home/user/documents", rootPath: "/home/user") { _ in }
}
