# file-exploder

サーバー側でファイル操作をキューイングする、macOS ネイティブの SSH リモートファイルマネージャです。

## 特徴

- **macOS ネイティブ UI** — SwiftUI 製で、Finder のような操作感
- **SSH** — 任意の Linux サーバーに SSH で接続
- **Finder ライクな画面** — 並べ替え・検索・複数選択に対応したリスト表示
- **サーバー側キュー** — クライアントが切断してもファイル操作は継続
- **操作の永続化** — キューはクライアントの切断後も残る
- **多様な操作** — 名前変更・移動・削除・コピー・ディレクトリ作成・権限変更
- **シンボリックリンク対応** — リンクはリンクとして表示し、リンク先がディレクトリなら
  通常のディレクトリと同じように辿れる

## 構成

```
┌─────────────────────────────────────────┐
│       macOS クライアント (Swift)        │
│  SwiftUI + SSH 経由のコマンド実行       │
└─────────────────┬───────────────────────┘
                  │ SSH
                  ▼
┌─────────────────────────────────────────┐
│           Linux サーバー (Go)           │
│  file-exploder デーモン + SQLite キュー │
└─────────────────────────────────────────┘
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

### クライアント (macOS)

```bash
cd Client
./build_mac.sh
cp -r file-exploder.app /Applications/
```

または `Client/Package.swift` を Xcode で開いてビルドします（Command+B）。

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

## 開発

### サーバー

```bash
cd Server
go mod tidy
go build -buildvcs=false -o file-exploder .
```

### クライアント

```bash
cd Client
swift build
```

## ライセンス

MIT
