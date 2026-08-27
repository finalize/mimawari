# mimawari

自分の個人開発を横断して見回り、**今、手を動かすべきこと**を1つにまとめて返す API。

```
$ mimawari -once

mimawari  2026-08-27 22:01:27  (1.2s, 4 プロジェクト)

手を動かすところ (1)
  action  termpic  PR #1 のチェックが 1 件落ちている（4日）
                   deps: bump bumpp from 11.1.0 to 12.2.1 — 落ちている: check & test & build
                   https://github.com/finalize/termpic/pull/1

プロジェクト
  shogo-site    site 200  110ms  pr 0   issue 2   npm -
  contrast-kit  site 200   94ms  pr 0   issue 0   npm 0.3.0 週542
  termpic       site 200   97ms  pr 1   issue 0   npm 0.2.2 週775
  hidori        site 200  164ms  pr 0   issue 0   npm -
```

## なぜ作ったか

Dependabot の PR が4日間、誰にも気づかれずに止まっていた。

`gh pr list` で見ると `mergeStateStatus: BLOCKED` としか出ない。これでは
**承認ボタンを押せば動くのか、CI が落ちていて直すところがあるのか**が分からない。
どちらも「blocked」だが、やることは正反対になる。

リポジトリが1つなら毎日開くから気づく。4つになると開かなくなる。
これから増えるほど、見に行かないと分からない状態は当たり前に取りこぼされる。
だから見に行く方を自動にした。

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

## 使う

```sh
go build -o mimawari ./cmd/mimawari

./mimawari -once              # 1回見回って端末に出して終わる
./mimawari -once -json        # JSON で出す
./mimawari                    # http://127.0.0.1:8787 で待ち受ける
```

毎朝これだけ見ればよい:

```sh
mimawari -once
```

CI やシェルから使うなら、`action` があるときだけ終了コードを 1 にできる:

```sh
mimawari -once -exit-code || say "見回りで何か出た"
```

### エンドポイント

| | |
|---|---|
| `GET /status` | 全プロジェクト（JSON） |
| `GET /status?format=text` | 端末で読む形 |
| `GET /status?fresh=1` | キャッシュを無視して取り直す |
| `GET /status/{project}` | 1プロジェクトだけ |
| `GET /attention` | 手を動かすところだけ |
| `GET /healthz` | |

`Accept: text/plain` でも端末向けの出力になる。

```sh
curl -s localhost:8787/status/termpic?format=text
curl -s localhost:8787/attention | jq -r '.attention[] | "\(.severity)\t\(.summary)"'
```

### フラグ

| | 既定 | |
|---|---|---|
| `-addr` | `127.0.0.1:8787` | 待ち受けるアドレス |
| `-config` | （埋め込みの既定値） | 設定ファイル |
| `-ttl` | `90s` | 結果を持ち回す時間 |
| `-timeout` | `30s` | 1回の見回りの制限時間 |
| `-once` | | 1回だけ見回って終わる |
| `-json` | | `-once` のときに JSON で出す |
| `-color` | `auto` | `auto` / `always` / `never` |
| `-exit-code` | | `-once` のとき、`action` があれば終了コード 1 |

### GitHub のトークン

`GITHUB_TOKEN` → `GH_TOKEN` → `gh auth token` の順に探す。
どれも無ければ未認証で動く（公開リポジトリだけ・レート上限は低い）。

## 設定

省略すると [`internal/config/default.json`](internal/config/default.json) を埋め込んだものを使う。
プロジェクトが増えたらここに足すか、`-config` で別のファイルを渡す。

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
問い合わせて 1.2 秒。直列だと 10 秒を超える。

**失敗もデータとして返す。** 集約する API が、1つの情報源が落ちただけで全部まとめて
失敗すると見回りとして使い物にならない。取れなかったものは `errors` に入れて、
取れたものはそのまま返す。

**結果は TTL で持ち回す。** 1回の見回りで GitHub に数十回問い合わせるので、
開き直すたびに走らせるとレート上限に当たる。取り直しのあいだはロックを握ったままにして、
同時に来た要求を並ばせている（外の API を同時に何度も叩かない）。

**依存はゼロ。** 標準ライブラリだけで書いてある。

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
