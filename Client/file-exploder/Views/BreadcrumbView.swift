import SwiftUI

struct BreadcrumbView: View {
    let path: String
    let onNavigate: (String) -> Void
    
    var pathComponents: [(String, String)] {
        var components: [(String, String)] = []
        let parts = path.split(separator: "/").map(String.init)
        
        var currentPath = ""
        for part in parts {
            currentPath += "/\(part)"
            components.append((part, currentPath))
        }
        
        return components
    }
    
    var body: some View {
        HStack(spacing: 4) {
            // Root
            Button(action: {
                // UIからHomeを押した場合でも、navigateTo内部で境界チェックが行われる
                onNavigate("/") 
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

#Preview {
    BreadcrumbView(path: "/home/user/documents") { _ in }
}
