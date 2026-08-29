# file-exploder

サーバー側でファイル操作をキューイングする、SSH リモートファイルマネージャです。macOS
ネイティブ (SwiftUI) と Windows (Avalonia UI) の 2 種類のクライアントがあり、どちらも同じ
サーバーデーモンに対して同じ機能を提供します。

## 特徴

- **ネイティブ UI** — macOS は SwiftUI、Windows は Avalonia UI 製で、それぞれの OS に
  馴染む操作感
- **SSH** — 任意の Linux サーバーに SSH で接続
- **Finder ライクな画面** — 並べ替え・検索・複数選択に対応したリスト表示
- **サーバー側キュー** — クライアントが切断してもファイル操作は継続
- **操作の永続化** — キューはクライアントの切断後も残る
- **多様な操作** — 名前変更・移動・削除・コピー・ディレクトリ作成・権限変更
- **安全なフォルダー統合** — 既存フォルダーを再帰的に統合し、競合ファイルは上書きしない
- **シンボリックリンク対応** — リンクはリンクとして表示し、リンク先がディレクトリなら
  通常のディレクトリと同じように辿れる
- **ドラッグ＆ドロップ** — 行をフォルダにドロップして移動。パンくずにドロップすれば
  上の階層へも移動できる

## 構成

```
  macOS クライアント (Swift)       Windows クライアント (C#)
  SwiftUI + SSH 経由の実行         Avalonia UI + SSH.NET
              \                         /
               \  SSH             SSH  /
                \                     /
                 v                   v
              Linux サーバー (Go)
              file-exploder デーモン + SQLite キュー
```

## インストール

### サーバー (Linux)

```bash
cd Server
chmod +x install.sh
./install.sh
```

インストーラは、macOS クライアントが SSH 接続に使うのと同じ Linux ユーザーで実行して
ください。そうすることで CLI とデーモンがファイルシステム上の権限と同じキューデータ
ベースを共有します。既知の標準ライブラリの脆弱性を避けるため、Go 1.26.7 以降が必要です。

- **通常の SSH ユーザーとして実行した場合（推奨）** — `~/.local/bin` にインストールし、
  ユーザー単位の systemd サービスを作成します。
- **`root` として実行した場合** — `/usr/local/bin` にインストールし、システムサービス
  `/etc/systemd/system/file-exploder.service` を作成します。このモードは、macOS
  クライアントも `root` で接続する場合にのみ使ってください。デーモンはサーバーの
  ファイルシステムに無制限にアクセスでき、キューは `/root/.file-exploder` に置かれます。

そのユーザーがログアウトした後もサービスを動かし続けるには、管理者が次を実行します。

```bash
sudo loginctl enable-linger <ssh-user>
```

通常ユーザーでのインストールは `systemctl --user status file-exploder`、root での
インストールは `systemctl status file-exploder` で状態を確認できます。クライアントに
設定するリモートルートは**画面上の移動範囲を決めるだけ**で、OS レベルのサンドボックス
ではありません。デーモンは SSH ユーザーとまったく同じ権限を持ちます。

各コマンドは JSON を標準出力に、エラーを標準エラー出力に書きます。そのためクライアントは
画面表示を解析することなく結果を読み取れます。キューの状態は `~/.file-exploder/queue.db`
にあります。

### キューの保存先を変える

`FILE_EXPLODER_DATA_DIR` でデータベース・ログ・デーモンのロックファイルの場所を変更でき
ますが、**デーモンと CLI の両方**に届く必要があります。両者は起動のされ方がまったく違い、
どちらもシェルの起動ファイルを読みません。systemd のユーザーマネージャは `~/.bashrc` を
読み込みませんし、既定の `~/.bashrc` は SSH コマンドが得る非対話シェルでは冒頭で return
します。片方だけに設定すると、デーモンと CLI が別々のキューを見ることになり、**すべての
操作が受理され、待機中と表示されたまま、永久に実行されません**。

サービスが読み込むファイルに書きます。

```bash
mkdir -p ~/.config/file-exploder
echo 'FILE_EXPLODER_DATA_DIR=/srv/file-exploder' > ~/.config/file-exploder/env
systemctl --user restart file-exploder
```

同じファイルを SSH セッションにも届かせます。`~/.bashrc` の冒頭にある非対話シェル用の
early return より**前**に置いてください。

```bash
set -a; . ~/.config/file-exploder/env; set +a
```

運用に乗せる前に、両者が一致していることを確認します。

```bash
systemctl --user show-environment | grep FILE_EXPLODER_DATA_DIR
ssh <host> 'echo $FILE_EXPLODER_DATA_DIR'
```

### ジョブのタイムアウトを変える

デーモンは 1 ジョブにつき既定で 24 時間待ちます。応答しないマウント上のファイル操作は
Go 側から中断できないため、これはハングしたジョブがキュー全体を永久に止めてしまわない
ようにするための上限であり、大きなファイルを遅い回線ごしにコピーするような正当に時間の
かかる操作を誤ってタイムアウトさせないよう、あえて長めに設定しています。デーモンだけが
読む設定なので、CLI 側に届かせる必要はありません。

```bash
echo 'FILE_EXPLODER_JOB_TIMEOUT=2h' >> ~/.config/file-exploder/env
systemctl --user restart file-exploder
```

### クライアント (macOS)

```bash
cd Client
./build_mac.sh
cp -r file-exploder.app /Applications/
```

または `Client/Package.swift` を Xcode で開いてビルドします（Command+B）。

### クライアント (Windows)

```powershell
cd WindowsClient
dotnet publish FileExploder\FileExploder.csproj -c Release -r win-x64 --self-contained false
```

`WindowsClient\FileExploder\bin\Release\net10.0\win-x64\publish\FileExploder.exe` に実行
ファイルが生成されます。.NET 10 ランタイムがインストール済みの環境向けで、単体で動かす
場合は `--self-contained true` を付けてください。

## 使い方

### SSH コマンド

```bash
# 操作をキューに追加する
file-exploder add --type rename --src /path/a --dst /path/b
file-exploder add --type move --src /path/a --dst /path/newdir/a
file-exploder add --type copy --src /path/a --dst /path/newdir/a
file-exploder add --type delete --src /path/file
file-exploder add --type mkdir --dst /path/newdir
file-exploder add --type chmod --dst /path/file --mode 755

# キューの状態を見る（待機中・実行中のジョブすべて、または ID 指定で 1 件）
file-exploder status
file-exploder status <job-id>

# ジョブをキャンセルする。キャンセルできるのは待機中のジョブだけです。
# デーモンがファイルに手を付け始めた後では、安全に巻き戻せる地点がありません。
file-exploder cancel <job-id>

# 最近終了したジョブを見る
file-exploder log

# ファイルシステムを JSON で調べる（macOS クライアントが使用）
file-exploder list /path/to/dir
file-exploder stat /path/to/file

# デーモンを起動する
file-exploder daemon
```

### macOS クライアント

1. file-exploder を起動する
2. 「+」をクリックしてサーバー接続を追加する
3. サーバーの情報を入力する（ホスト名・ユーザー名・SSH キーのパス）
4. サーバーをクリックして接続する
5. ファイルを閲覧・操作する

ファイルやフォルダは、行をフォルダの行にドラッグ＆ドロップして移動できます。上の階層へ
移動したいときは、パンくずの該当する部分にドロップしてください。複数選択しているときは、
そのうちの 1 行をドラッグすれば選択中のすべてが移動します（Finder と同じ挙動です）。移動
先がすでに同じフォルダの場合や、フォルダを自分自身の中へ入れようとした場合は、ドロップが
受け付けられません。

### Windows クライアント

操作方法は macOS クライアントと同じです。サーバー一覧・ファイル一覧・ドラッグ＆ドロップ・
キューパネル・設定（隠しファイルの表示・更新間隔）まで、機能は一対一で対応しています。
「ファイル」メニューの「新しいウィンドウ」(Ctrl+N) で複数ウィンドウを開けます。それぞれ
別のサーバーに接続できますが、保存済みサーバーの一覧と設定はウィンドウ間で共有されます。

## 開発

### サーバー

```bash
cd Server
go mod tidy
go build -buildvcs=false -o file-exploder .
```

### クライアント (macOS)

```bash
cd Client
swift build
```

### クライアント (Windows)

.NET 10 SDK があれば、macOS/Linux 上でもビルド・テストできます（Windows 実行ファイルは
生成できますが、当然そこでは実行できません）。

```bash
cd WindowsClient
dotnet build FileExploder.slnx
dotnet test FileExploder.slnx
```

`FileExploder.Tests` の一部（SSH 接続・SFTPService・ViewModel・UI のテスト）は、実際に
ローカルの `sshd` と `file-exploder` デーモンに接続して検証します。両方が起動しているマシン
でのみ実行してください。

## ライセンス

MIT
