~/ghq/github.com/walnuts1018/picca を参考に、

- Dockerfileを最新機能で改善する
- GitHub ActionsでImageをビルドしてpushできるようにする。
  - ただし、releaseとは関係なく、mainブランチpush時に自動ビルドしてpushする。GitHub TagとかReleaseによるバージョン管理は不要。ただしimage tagはいい感じにsemverっぽくする（renovate用）
- push時、attestation/provenance/sbomなども適切につける
