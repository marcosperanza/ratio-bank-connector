# Third-party notices

This release of connector includes Go modules used in its supported Linux, macOS, and Windows builds. Dependency license texts are included in [`THIRD_PARTY_LICENSES/`](THIRD_PARTY_LICENSES/). The complete source of the MPL-2.0 HCL dependency is preserved in [`.third_party-source/github.com/hashicorp/hcl/`](.third_party-source/github.com/hashicorp/hcl/). The Go standard-library/toolchain license is also included there. The project itself is licensed separately under AGPL-3.0-only; third-party components retain their own licenses.

| Component | License | Included target(s) | License/source reference |
|---|---|---|---|
| `github.com/ably/ably-go/ably` | Apache-2.0 | darwin, linux, windows | [upstream](https://github.com/ably/ably-go/blob/v1.4.1/LICENSE) |
| `github.com/ably/vcdiff-go` | Apache-2.0 | darwin, linux, windows | [upstream](https://github.com/ably/vcdiff-go/blob/v0.0.2/LICENSE) |
| `github.com/coder/websocket` | ISC | darwin, linux, windows | [upstream](https://github.com/coder/websocket/blob/v1.8.12/LICENSE.txt) |
| `github.com/fsnotify/fsnotify` | BSD-3-Clause | darwin, linux, windows | [upstream](https://github.com/fsnotify/fsnotify/blob/v1.7.0/LICENSE) |
| `github.com/hashicorp/hcl` | MPL-2.0 | darwin, linux, windows | [upstream](https://github.com/hashicorp/hcl/blob/v1.0.0/LICENSE) |
| `github.com/inconshreveable/mousetrap` | Apache-2.0 | windows | [upstream](https://github.com/inconshreveable/mousetrap/blob/v1.1.0/LICENSE) |
| `github.com/magiconair/properties` | BSD-2-Clause | darwin, linux, windows | [upstream](https://github.com/magiconair/properties/blob/v1.8.7/LICENSE.md) |
| `github.com/mitchellh/mapstructure` | MIT | darwin, linux, windows | [upstream](https://github.com/mitchellh/mapstructure/blob/v1.5.0/LICENSE) |
| `github.com/pelletier/go-toml/v2` | MIT | darwin, linux, windows | [upstream](https://github.com/pelletier/go-toml/blob/v2.2.2/LICENSE) |
| `github.com/sagikazarmark/slog-shim` | BSD-3-Clause | darwin, linux, windows | [upstream](https://github.com/sagikazarmark/slog-shim/blob/v0.1.0/LICENSE) |
| `github.com/spf13/afero` | Apache-2.0 | darwin, linux, windows | [upstream](https://github.com/spf13/afero/blob/v1.11.0/LICENSE.txt) |
| `github.com/spf13/cast` | MIT | darwin, linux, windows | [upstream](https://github.com/spf13/cast/blob/v1.6.0/LICENSE) |
| `github.com/spf13/cobra` | Apache-2.0 | darwin, linux, windows | [upstream](https://github.com/spf13/cobra/blob/v1.9.1/LICENSE.txt) |
| `github.com/spf13/pflag` | BSD-3-Clause | darwin, linux, windows | [upstream](https://github.com/spf13/pflag/blob/v1.0.6/LICENSE) |
| `github.com/spf13/viper` | MIT | darwin, linux, windows | [upstream](https://github.com/spf13/viper/blob/v1.19.0/LICENSE) |
| `github.com/subosito/gotenv` | MIT | darwin, linux, windows | [upstream](https://github.com/subosito/gotenv/blob/v1.6.0/LICENSE) |
| `github.com/ugorji/go/codec` | MIT | darwin, linux, windows | [upstream](https://github.com/ugorji/go/blob/codec/v1.1.9/codec/LICENSE) |
| `go.uber.org/multierr` | MIT | darwin, linux, windows | [upstream](https://github.com/uber-go/multierr/blob/v1.10.0/LICENSE.txt) |
| `go.uber.org/zap` | MIT | darwin, linux, windows | [upstream](https://github.com/uber-go/zap/blob/v1.27.0/LICENSE) |
| `golang.org/x/crypto/pbkdf2` | BSD-3-Clause | darwin, linux, windows | [upstream](https://cs.opensource.google/go/x/crypto/+/v0.31.0:LICENSE) |
| `golang.org/x/sys/unix` | BSD-3-Clause | darwin, linux | [upstream](https://cs.opensource.google/go/x/sys/+/v0.28.0:LICENSE) |
| `golang.org/x/sys/windows` | BSD-3-Clause | windows | [upstream](https://cs.opensource.google/go/x/sys/+/v0.28.0:LICENSE) |
| `golang.org/x/term` | BSD-3-Clause | darwin, linux, windows | [upstream](https://cs.opensource.google/go/x/term/+/v0.27.0:LICENSE) |
| `golang.org/x/text` | BSD-3-Clause | darwin, linux, windows | [upstream](https://cs.opensource.google/go/x/text/+/v0.21.0:LICENSE) |
| `gopkg.in/ini.v1` | Apache-2.0 | darwin, linux, windows | [upstream](https://github.com/go-ini/ini/blob/v1.67.0/LICENSE) |
| `gopkg.in/yaml.v3` | MIT | darwin, linux, windows | [upstream](https://github.com/go-yaml/yaml/blob/v3.0.1/LICENSE) |

The report was generated from the release build graph for `linux`, `darwin`, and `windows`. Assembly and cgo source can require manual inspection beyond automated package-level scanning; the license of each complete module was reviewed and the applicable files are preserved in the bundle.
