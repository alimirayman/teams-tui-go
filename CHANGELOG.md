# Changelog

## [0.9.0] - 2026-05-26

### Features

- **More messages and chats** - ([3f6080f](3f6080fd82ce81dffd6e9bb69de49fee92e94391))


- **Creating default config** - ([ecd939c](ecd939c73410899fc58bd9dad96708edfdf8b660))


- **Search chats** - ([1a8fe9c](1a8fe9c93b3530e584a5e3f702df6212871fa28b))


- **Indicators for chats with new reactions** - ([f99e832](f99e832b389c61f3777e9db2f0a7fd6bca05c3db))



### Bug Fixes

- **Improve chat read tracking, notification logic, and reload stability** - ([b3c7ab1](b3c7ab1b2299c1650bacc76e3e48e68183ce3af7))


- Set default HTTP client timeout to 15s to prevent hanging background
requests.
- Avoid returning nil from background chat loaders on token refresh/API
error; fallback to existing chat states instead.
- Refine unread message detection using last message times and a
1-second threshold to prevent duplicate notification triggers.
- Mark the active chat as read on the server when updates arrive and the
terminal is focused.
- Ensure proper tracking of `LastUpdated` times in initial chat
ordering.


- **Reactions order** - ([cd334a9](cd334a9d1edd2a097511d0cb5d01f0af68b1d9b9))



### Miscellaneous Tasks

- **Remove boilerplate sentence from changelog header** - ([8230583](8230583445d816fa6bec275daffcdb94ea3d7635))


- **Update CHANGELOG.md for v0.9.0 [skip ci]** - ([579cdaa](579cdaa319f4f8337dc777b0c219691bc2259d14))


- **Suppress Node 20 deprecation warnings and commit CHANGELOG.md to repo** - ([42725c4](42725c4b9c59a3981bfdeaaf7cef16f57fe38259))


- **Revert to standard GitHub Actions now that suspension is lifted** - ([d1d5500](d1d5500578cbcab23149c4475f8d9f0410cf2e0d))


- **Bypass broken github actions by using golang container and wget** - ([496a60a](496a60a0b9a3d92509465623b5e7fbd96423a3a1))


- **Upgrade actions to v5/v6 and force Node 24 to fix runner downloads** - ([aae325d](aae325db8ee50d9a65c14a816d6a2861bafb62fd))


- **Bump git-cliff-action to v4 to bypass GitHub CDN errors** - ([d7ae7d1](d7ae7d16a97922e12c75e9a58df30e063369e32f))


- **Downgrade setup-go to v4 to mitigate GitHub CDN tarball errors** - ([6ddbfa3](6ddbfa33b9d4028ef5cb95458302fd3391744b95))


- **Setup GitHub Actions for testing, releases, and changelogs** - ([81fea23](81fea23cb8b520a80a4dab50ae91fd87785c949c))


- Add test workflow (`test.yml`) to run Go tests automatically on pushes
and PRs to main
- Add release workflow (`release.yml`) to build multi-platform binaries
(Linux, macOS, Windows) and publish GitHub Releases automatically when
pushing version tags (`v*`)
- Introduce `cliff.toml` to automate conventional changelog generation
via `git-cliff`, formatting commit bodies using native Markdown newlines
- Inject dynamic release versions into the binary during the CI build
process and print the version in the application's startup banner



## [0.8.7] - 2026-05-21

### Bug Fixes

- **Grouping messages by hour** - ([855dce7](855dce7ac1a811d77d4d819abee6bf960e37fe8a))


- **Empty meetings problems** - ([519d8d1](519d8d12fbb73125f5eda5dff3b36738a6e44a3f))


- **Some messages sorting** - ([ef2c95a](ef2c95a54d9f227f0cb1146c631f7a60372ddaf8))



## [0.8.6] - 2026-05-19

### Features

- **Search chats** - ([d45cd0a](d45cd0a2faf0daf2e13bcba5cd345b6ddc35cdc6))



## [0.8.5] - 2026-05-12

### Features

- **Emoticons in composing message** - ([fcb8a53](fcb8a53ab3578748b004532a4ec572864be85e83))


- **Urls** - ([db86d81](db86d8107bec58449dcc4461b831e2b19eb214fd))


- **Showing urls** - ([c7f5065](c7f50657b6cbcb342f710a3a827c7ab37d936341))


- **Emoticons when writing messages** - ([84a740f](84a740f7cb8b7401372f13d6006bd56d1a9c2ae9))



### Bug Fixes

- **Message mode starts now when user left** - ([980e48b](980e48bbf4a05da4615c9ac2c76cab9562e85f26))



## [0.8.3] - 2026-05-12

### Features

- **Answer to messages** - ([8643997](8643997bc2436fc6104480d9d513092e5a82f271))


- **Update/edit messages** - ([1b30aca](1b30aca9ec093c3a40f128aae7b0c7e1b75f9d17))



## [0.8.2] - 2026-05-11

### Features

- **Loading history** - ([77fc082](77fc08222920f079563a6f413bc309a6f0701bc8))



## [0.8.1] - 2026-05-11

### Features

- **Clearing copy messages** - ([bd1c224](bd1c2249ca34546cc9e19e4caf1dbf934f656c75))


- **Yank(copy) message** - ([ec3a6ae](ec3a6ae567ee4fe3a81294e19a82ee06ffe858a1))



## [0.8] - 2026-05-11

### Features

- **Message limit** - ([801a57c](801a57c441a2ed5c4fb4396e12dc8d18cfa22612))



## [0.7.9] - 2026-05-11

### Features

- **Delete messages** - ([6adae23](6adae2351922fec6e2fc7d57e49ab9eca4229b54))



## [0.7.8] - 2026-05-11

### Features

- **Adding reactions to messages** - ([4a7ac0c](4a7ac0cae34866aeb7ec63b8f1a9650e3102893a))


- **Add reaction to a message** - ([93709c9](93709c93c7c1af821530cbe5ef5834a6d3452bc8))


- **Messages interactions** - ([aeca458](aeca4585a4b38fcead7982e6be71ed0c4a646cf5))



### Bug Fixes

- **Normal multiline text** - ([08ec699](08ec6994b15b7d334766e13a006cb3ed11ee777e))



## [0.7.7] - 2026-05-08

### Bug Fixes

- **Wrong formatted text** - ([f814c6d](f814c6d37a3658b9c27a645bc71aa5a237cd762f))



## [0.7.6] - 2026-05-08

### Bug Fixes

- **Not readchat after sending messsage** - ([bccf3cf](bccf3cfcafd6b7e63f94000163d7afa6d0dadf0d))



## [0.7.5] - 2026-05-08

### Features

- **Notification preview** - ([6cc856e](6cc856ecfa58b9e9ecd00154469645d25bdb1fe5))



### Bug Fixes

- **Not read messages** - ([e274b02](e274b022171051cda105af9f5724b175c45344f1))


- **Local time** - ([daa6e69](daa6e69d9d988332b35e4d3d17c8c69e955eff14))


- **Attachments view** - ([d49ad97](d49ad97f7c03c09c03cfd7888859e33ca28e1e2f))



## [0.7] - 2026-05-07

### Bug Fixes

- **Sorting chats** - ([50431eb](50431eb7b317015d9cede33de558f68266781b00))


- **Names in group chats** - ([ae38ea7](ae38ea73bf3100eac1b1006691f3e745cdccc987))


- **Fixing some initial problems** - ([6b10473](6b10473a3c38c797c073738011334b9707d60b86))



### Other

- **Readme** - ([c7ac8cf](c7ac8cfab35bcbe55b095ff4fa57f2baf73e51ed))


- **Init first version** - ([bdd0271](bdd027164e42e4f7a22c849dd1bc9ed42dddd030))


- **Initial commit** - ([f30e39e](f30e39e2f191df8142a594c5482557760a42025e))




