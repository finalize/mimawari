# mimawari

自分の個人開発を横断して見回り、**今、手を動かすべきこと**を1つにまとめて出すコマンド。

```
$ mimawari

mimawari  2026-08-27 23:46:45  (1.3s, 4 プロジェクト)

手を動かすところ (1)
  action  termpic  PR #1 のチェックが 1 件落ちている（4日）
                   deps: bump bumpp from 11.1.0 to 12.2.1 — 落ちている: check & test & build
                   https://github.com/finalize/termpic/pull/1

プロジェクト
  shogo-site    site 200  225ms  pr 0   issue 2   npm -
  contrast-kit  site 200  396ms  pr 0   issue 0   npm 0.3.0 週542
  termpic       site 200  217ms  pr 1   issue 0   npm 0.2.2 週775
  hidori        site 200  396ms  pr 0   issue 0   npm -
```

## なぜ作ったか

Dependabot の PR が4日間、誰にも気づかれずに止まっていた。

`gh pr list` で見ると `mergeStateStatus: BLOCKED` としか出ない。これでは
**承認ボタンを押せば動くのか、CI が落ちていて直すところがあるのか**が分からない。
どちらも「blocked」だが、やることは正反対になる。

リポジトリが1つなら毎日開くから気づく。4つになると開かなくなる。
これから増えるほど、見に行かないと分からない状態は当たり前に取りこぼされる。
だから見に行く方を自動にした。

## 入れる

```sh
go build -o ~/.local/bin/mimawari ./cmd/mimawari
```

`~/.local/bin` が PATH に入っていれば、これで `mimawari` として使える。
GitHub のトークンは `GITHUB_TOKEN` → `GH_TOKEN` → `gh auth token` の順に探すので、
`gh` にログイン済みなら準備は要らない。どれも無ければ未認証で動く
（公開リポジトリだけ・レート上限は低い）。

## 使う

```sh
mimawari                # 見回って出す
mimawari -json          # JSON で出す
mimawari -quiet         # 手を動かすところだけタブ区切りで出す
```

毎朝の1コマンドは `mimawari` だけ。

### 何か出たときだけ動く

`-exit-code` を付けると、`action` が1件でもあれば終了コードが `1` になる。

```sh
mimawari -exit-code > /dev/null || mimawari
mimawari -exit-code > /dev/null || say "見回りで何か出た"
```

### 他のコマンドに流す

`-quiet` は `重さ / プロジェクト / 内容` のタブ区切り。
色は端末に出すときだけ付くので、パイプに流しても崩れない。

```sh
mimawari -quiet | awk -F'\t' '$1 == "action" { print $2 }'
mimawari -json | jq -r '.attention[] | "\(.severity)\t\(.summary)"'
mimawari -json | jq '.projects[] | select(.npm.drift)'
```

### フラグ

| | 既定 | |
|---|---|---|
| `-config` | （埋め込みの既定値） | 設定ファイル |
| `-timeout` | `30s` | 1回の見回りの制限時間 |
| `-json` | | JSON で出す |
| `-quiet` | | 手を動かすところだけ出す |
| `-color` | `auto` | `auto` / `always` / `never` |
| `-exit-code` | | `action` があれば終了コード 1 |

## 何を見るか

| 情報源 | 見ているもの |
|---|---|
| GitHub | open な PR とその止まっている理由・open な issue・最新リリースからの進み具合 |
| npm | 公開済みバージョンと手元の `package.json` の差・先週のダウンロード数 |
| サイト | 公開している URL が返事をするか・応答までの時間 |

### PR が止まっている理由の分類

`mergeable_state` だけでは足りないので、チェックの内訳とワークフローの状態まで見て1つに決める。
先に見たものが勝つ。**直し方が違うものほど先**に置いてある。

| 状態 | 意味 | やること |
|---|---|---|
| `needs-approval` | ワークフローが承認待ち | 承認ボタンを押す |
| `conflict` | 競合している | 手元で解消する |
| `checks-failing` | チェックが落ちている | 落ちたチェックを直す（名前まで出る） |
| `behind` | base に追いついていない | 更新する |
| `checks-pending` | チェックが走っている | 待つ（長引いたときだけ知らせる） |
| `blocked` | 落ちても待ってもいないのに blocked | 必須チェックが始まっていないか、レビューが足りない |
| `mergeable` | マージできる | マージする（自動マージが有効なら知らせない） |
| `draft` | 下書き | 何もしない |

`needs-approval` はチェック一覧に出ないことがあるので、ワークフローの実行状態
（`status: action_required`）も併せて見ている。

### 重さ

- `action` — 人が動かないと先に進まない
- `warn` — 放っておくと困るが、まだ動いている
- `info` — 知っておくと良い程度

## 設定

省略すると [`internal/config/default.json`](internal/config/default.json) を埋め込んだものを使う。
プロジェクトが増えたらここに足すか、`-config` で別のファイルを渡す。
**埋め込みはビルド時なので、`default.json` を変えたら建て直しが要る。**

```json
{
  "workspace": "~/workspace",
  "thresholds": {
    "stale_pr_hours": 24,
    "stale_issue_days": 14,
    "slow_site_ms": 1500
  },
  "projects": [
    {
      "name": "termpic",
      "repo": "finalize/termpic",
      "branch": "main",
      "site": "https://termpic.shgysd.workers.dev",
      "npm": "termpic",
      "manifest": "termpic/packages/termpic/package.json"
    }
  ]
}
```

- `repo` 以外は省略できる。`name` は `repo` から、`branch` は `main` が入る
- `manifest` は `workspace` からの相対パス。npm の公開済みバージョンと突き合わせて、
  publish し忘れ・上げ忘れを見つけるために使う
- 知らないキーがあると読み込みで失敗する（打ち間違いを黙って無視しない）

## 作りの要点

**並列に取る。** プロジェクトごとに goroutine を立て、その中で GitHub・npm・サイトの
3系統をさらに並列に、PR は1件ずつさらに並列に取る。4プロジェクトで 30 回ほど
問い合わせて 1.3 秒。1回 200〜400ms かかるので、直列に並べれば 6〜10 秒になる計算。

**失敗もデータとして返す。** 集約する道具が、1つの情報源が落ちただけで全部まとめて
失敗すると見回りにならない。取れなかったものは `errors` に入れて、
取れたものはそのまま出す。

**依存はゼロ。** 標準ライブラリだけで書いてある。

**HTTP API ではなく CLI。** 最初は `/status` を返すサーバとして書いたが、
使うのは1日1回の1コマンドで、TTL キャッシュもエンドポイントの出し分けも効いていなかった。
サーバを持つと置き場所（Cloudflare Workers は Go を動かせない）まで抱えることになるので、
やめた。当時の実装はコミット `4b8220c` に残してある。

## 開発

```sh
go test ./...
go test -race ./...
go vet ./...
gofmt -l .
```

テストは実際のネットワークに出ない。GitHub・npm・サイトはすべて `httptest` の
偽サーバに差し替えている。

## ライセンス

MIT
