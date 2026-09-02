# 第三方许可证

本文件汇总本项目分发构建中直接使用的第三方依赖。
它是工程合规文档，应与 Docker 镜像、独立二进制文件、前端构建产物和 Electron 安装包一同保留。

范围：来自 `go.mod`、`web/package.json` 和 `electron/package.json` 的直接依赖。
在正式对外发布前，还应审查传递依赖。

## 依赖清单

| 区域 | 范围 | 生态系统 | 依赖项 | 版本 | 许可证 |
|-------------|-------------|-----------|-------------------------------------------------------|--------------------------------------|----------------------------------------------------|
| 后端 | 生产环境 | Go | `github.com/abema/go-mp4` | `v1.4.1` | MIT |
| 后端 | 测试环境 | Go | `github.com/alicebob/miniredis/v2` | `v2.38.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/andybalholm/brotli` | `v1.1.1` | MIT |
| 后端 | 生产环境 | Go | `github.com/anknown/ahocorasick` | `v0.0.0-20190904063843-d75dbd5169c0` | MIT |
| 后端 | 生产环境 | Go | `github.com/aws/aws-sdk-go-v2` | `v1.41.5` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/aws/aws-sdk-go-v2/credentials` | `v1.19.10` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/aws/aws-sdk-go-v2/service/bedrockruntime` | `v1.50.4` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/aws/smithy-go` | `v1.24.2` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/bytedance/gopkg` | `v0.1.3` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/gin-contrib/cors` | `v1.7.2` | MIT |
| 后端 | 生产环境 | Go | `github.com/gin-contrib/gzip` | `v0.0.6` | MIT |
| 后端 | 生产环境 | Go | `github.com/gin-contrib/static` | `v0.0.1` | MIT |
| 后端 | 生产环境 | Go | `github.com/gin-gonic/gin` | `v1.9.1` | MIT |
| 后端 | 生产环境 | Go | `github.com/glebarez/sqlite` | `v1.9.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/go-audio/aiff` | `v1.1.0` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/go-audio/wav` | `v1.1.0` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/go-playground/validator/v10` | `v10.20.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/go-redis/redis/v8` | `v8.11.5` | BSD-2-Clause |
| 后端 | 生产环境 | Go | `github.com/golang-jwt/jwt/v5` | `v5.3.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/google/uuid` | `v1.6.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `github.com/gorilla/websocket` | `v1.5.0` | BSD-2-Clause |
| 后端 | 生产环境 | Go | `github.com/grafana/pyroscope-go` | `v1.2.7` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/grafana/sobek` | `v0.0.0-20260708062710-267a0e055bb4` | MIT |
| 后端 | 生产环境 | Go | `github.com/jfreymuth/oggvorbis` | `v1.0.5` | MIT |
| 后端 | 生产环境 | Go | `github.com/jinzhu/copier` | `v0.4.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/joho/godotenv` | `v1.5.1` | MIT |
| 后端 | 生产环境 | Go | `github.com/mewkiz/flac` | `v1.0.13` | Unlicense |
| 后端 | 生产环境 | Go | `github.com/nicksnyder/go-i18n/v2` | `v2.6.1` | MIT |
| 后端 | 测试环境 | Go | `github.com/openai/openai-go` | `v1.12.0` | Apache-2.0 |
| 后端 | 生产环境 | Go | `github.com/pkg/errors` | `v0.9.1` | BSD-2-Clause |
| 后端 | 生产环境 | Go | `github.com/samber/hot` | `v0.11.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/samber/lo` | `v1.52.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/shirou/gopsutil` | `v3.21.11+incompatible` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `github.com/shopspring/decimal` | `v1.4.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/stretchr/testify` | `v1.11.1` | MIT |
| 后端 | 生产环境 | Go | `github.com/tcolgate/mp3` | `v0.0.0-20170426193717-e79c5a46d300` | MIT |
| 后端 | 生产环境 | Go | `github.com/thanhpk/randstr` | `v1.0.6` | MIT |
| 后端 | 生产环境 | Go | `github.com/tidwall/gjson` | `v1.18.0` | MIT |
| 后端 | 生产环境 | Go | `github.com/tidwall/sjson` | `v1.2.5` | MIT |
| 后端 | 生产环境 | Go | `github.com/tiktoken-go/tokenizer` | `v0.6.2` | MIT |
| 后端 | 生产环境 | Go | `github.com/yapingcat/gomedia` | `v0.0.0-20240906162731-17feea57090c` | MIT |
| 后端 | 生产环境 | Go | `golang.org/x/crypto` | `v0.45.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `golang.org/x/image` | `v0.38.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `golang.org/x/net` | `v0.47.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `golang.org/x/sync` | `v0.20.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `golang.org/x/sys` | `v0.38.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `golang.org/x/text` | `v0.35.0` | BSD-3-Clause |
| 后端 | 生产环境 | Go | `gopkg.in/yaml.v3` | `v3.0.1` | Apache-2.0 OR MIT |
| 后端 | 生产环境 | Go | `gorm.io/driver/mysql` | `v1.4.3` | MIT |
| 后端 | 生产环境 | Go | `gorm.io/driver/postgres` | `v1.5.2` | MIT |
| 后端 | 生产环境 | Go | `gorm.io/gorm` | `v1.25.2` | MIT |
| 后端 | 生产环境 | Go | `github.com/expr-lang/expr` | `v1.17.8` | MIT |
| 前端 | 生产环境 | npm | `@base-ui/react` | `1.6.0` | MIT |
| 前端 | 生产环境 | npm | `@codemirror/lang-javascript` | `6.2.5` | MIT |
| 前端 | 生产环境 | npm | `@codemirror/lang-markdown` | `6.5.1` | MIT |
| 前端 | 生产环境 | npm | `@codemirror/language` | `6.12.4` | MIT |
| 前端 | 生产环境 | npm | `@codemirror/state` | `6.7.1` | MIT |
| 前端 | 生产环境 | npm | `@codemirror/view` | `6.43.6` | MIT |
| 前端 | 生产环境 | npm | `@fontsource-variable/lora` | `5.3.0` | OFL-1.1 |
| 前端 | 生产环境 | npm | `@fontsource-variable/public-sans` | `5.3.0` | OFL-1.1 |
| 前端 | 生产环境 | npm | `@hookform/resolvers` | `5.4.0` | MIT |
| 前端 | 生产环境 | npm | `@hugeicons/core-free-icons` | `4.2.2` | MIT |
| 前端 | 生产环境 | npm | `@hugeicons/react` | `1.1.9` | MIT |
| 前端 | 生产环境 | npm | `@lezer/highlight` | `1.2.3` | MIT |
| 前端 | 生产环境 | npm | `@lobehub/icons` | `5.14.0` | MIT |
| 前端 | 生产环境 | npm | `@tanstack/react-query` | `5.101.2` | MIT |
| 前端 | 生产环境 | npm | `@tanstack/react-router` | `1.170.18` | MIT |
| 前端 | 生产环境 | npm | `@tanstack/react-table` | `8.21.3` | MIT |
| 前端 | 生产环境 | npm | `@tanstack/react-virtual` | `3.14.6` | MIT |
| 前端 | 生产环境 | npm | `@visactor/react-vchart` | `2.1.4` | MIT |
| 前端 | 生产环境 | npm | `@visactor/vchart` | `2.1.4` | MIT |
| 前端 | 生产环境 | npm | `ai` | `7.0.31` | Apache-2.0 |
| 前端 | 生产环境 | npm | `auto-skeleton-react` | `1.0.5` | MIT |
| 前端 | 生产环境 | npm | `axios` | `1.18.1` | MIT |
| 前端 | 生产环境 | npm | `class-variance-authority` | `0.7.1` | Apache-2.0 |
| 前端 | 生产环境 | npm | `clsx` | `2.1.1` | MIT |
| 前端 | 生产环境 | npm | `cmdk` | `1.1.1` | MIT |
| 前端 | 生产环境 | npm | `dayjs` | `1.11.21` | MIT |
| 前端 | 生产环境 | npm | `dompurify` | `3.4.11` | Apache-2.0 OR MPL-2.0 |
| 前端 | 生产环境 | npm | `i18next` | `26.3.6` | MIT |
| 前端 | 生产环境 | npm | `i18next-browser-languagedetector` | `8.2.1` | MIT |
| 前端 | 生产环境 | npm | `katex` | `0.17.0` | MIT |
| 前端 | 生产环境 | npm | `lucide-react` | `1.25.0` | ISC |
| 前端 | 生产环境 | npm | `marked` | `18.0.6` | MIT |
| 前端 | 生产环境 | npm | `motion` | `12.42.2` | MIT |
| 前端 | 生产环境 | npm | `nanoid` | `5.1.16` | MIT |
| 前端 | 生产环境 | npm | `next-themes` | `0.4.6` | MIT |
| 前端 | 生产环境 | npm | `react` | `19.2.7` | MIT |
| 前端 | 生产环境 | npm | `react-day-picker` | `10.0.1` | MIT |
| 前端 | 生产环境 | npm | `react-dom` | `19.2.7` | MIT |
| 前端 | 生产环境 | npm | `react-hook-form` | `7.82.0` | MIT |
| 前端 | 生产环境 | npm | `react-i18next` | `17.0.10` | MIT |
| 前端 | 生产环境 | npm | `react-icons` | `5.7.0` | MIT |
| 前端 | 生产环境 | npm | `react-resizable-panels` | `4.12.2` | MIT |
| 前端 | 生产环境 | npm | `react-top-loading-bar` | `3.0.2` | MIT |
| 前端 | 生产环境 | npm | `recharts` | `3.9.1` | MIT |
| 前端 | 生产环境 | npm | `shiki` | `4.3.1` | MIT |
| 前端 | 生产环境 | npm | `sonner` | `2.0.7` | MIT |
| 前端 | 生产环境 | npm | `sse.js` | `2.8.0` | Apache-2.0 |
| 前端 | 生产环境 | npm | `stream-markdown-parser` | `1.1.3` | MIT |
| 前端 | 生产环境 | npm | `tailwind-merge` | `3.6.0` | MIT |
| 前端 | 生产环境 | npm | `tailwindcss` | `4.3.3` | MIT |
| 前端 | 生产环境 | npm | `tokenlens` | `1.3.1` | MIT |
| 前端 | 生产环境 | npm | `tw-animate-css` | `1.4.0` | MIT |
| 前端 | 生产环境 | npm | `use-stick-to-bottom` | `1.1.6` | MIT |
| 前端 | 生产环境 | npm | `vaul` | `1.1.2` | MIT |
| 前端 | 生产环境 | npm | `zod` | `4.4.3` | MIT |
| 前端 | 生产环境 | npm | `zustand` | `5.0.14` | MIT |
| 前端 | 开发环境 | npm | `@rsbuild/core` | `2.1.6` | MIT |
| 前端 | 开发环境 | npm | `@rsbuild/plugin-react` | `2.1.0` | MIT |
| 前端 | 开发环境 | npm | `@rsbuild/plugin-tailwindcss` | `2.0.3` | MIT |
| 前端 | 开发环境 | npm | `@tanstack/react-query-devtools` | `5.101.2` | MIT |
| 前端 | 开发环境 | npm | `@tanstack/react-router-devtools` | `1.167.0` | MIT |
| 前端 | 开发环境 | npm | `@tanstack/router-plugin` | `1.168.23` | MIT |
| 前端 | 开发环境 | npm | `@types/node` | `26.1.1` | MIT |
| 前端 | 开发环境 | npm | `@types/react` | `19.2.17` | MIT |
| 前端 | 开发环境 | npm | `@types/react-dom` | `19.2.3` | MIT |
| 前端 | 开发环境 | npm | `@typescript/native-preview` | `7.0.0-dev.20260707.2` | Apache-2.0 |
| 前端 | 开发环境 | npm | `@xyflow/react` | `12.11.2` | MIT |
| 前端 | 开发环境 | npm | `embla-carousel-react` | `8.6.0` | MIT |
| 前端 | 开发环境 | npm | `knip` | `6.27.0` | ISC |
| 前端 | 开发环境 | npm | `oxfmt` | `0.57.0` | MIT |
| 前端 | 开发环境 | npm | `oxlint` | `1.74.0` | MIT |
| 前端 | 开发环境 | npm | `shadcn` | `4.13.1` | MIT |
| Electron | 开发环境 | npm | `cross-env` | `7.0.3` | MIT |
| Electron | 开发环境 | npm | `electron` | `39.8.5` | MIT |
| Electron | 开发环境 | npm | `electron-builder` | `26.7.0` | MIT |

## 许可证正文

以下标准许可证正文保留官方英文原文，以确保分发和合规时的法律文本完整；章节标题和说明已翻译为中文。

### Apache-2.0（Apache 许可证 2.0）

以下保留 Apache License 2.0 的官方英文许可证正文。

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

### Apache-2.0 OR MIT（双重许可）

采用双重许可的组件可以选择 Apache-2.0 或 MIT。以下同时保留两份标准许可证的官方英文正文。

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### BSD-2-Clause（BSD 2-Clause 许可证）

BSD 2-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### BSD-3-Clause（BSD 3-Clause 许可证）

BSD 3-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### ISC（ISC 许可证）

ISC License

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.

### MIT（MIT 许可证）

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### OFL-1.1（SIL 开放字体许可证 1.1）

SIL Open Font License 1.1

列在 OFL-1.1 下的字体依赖使用 SIL 开放字体许可证 1.1。完整许可证正文请参阅：
https://openfontlicense.org/open-font-license-official-text/

分发字体文件时，请保留上游字体项目提供的 OFL 许可证正文、版权声明以及保留字体名称限制。

### 专有/内部依赖 - 由项目维护者所有

该依赖由项目维护者所有，本次审查不将其视为第三方开源依赖。

### Unlicense（无条件许可）

以下保留 The Unlicense 的官方英文正文。

The Unlicense

This is free and unencumbered software released into the public domain.
Anyone is free to copy, modify, publish, use, compile, sell, or distribute
this software, either in source code form or as a compiled binary, for any
purpose, commercial or non-commercial, and by any means.

For more information, please refer to https://unlicense.org/
