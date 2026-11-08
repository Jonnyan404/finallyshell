# Changelog

Each version's notes, newest first, in English and Simplified Chinese. A release's notes on GitHub and in the app's update window are its section here; see [Releasing](docs/en/development.md#releasing).

## 0.2.1

### English

- Finallyshell now checks for new versions and updates itself in place. It checks automatically, which you can turn off in Options › General, and "Check for Updates…" is in Options › General, in the tray icon's menu and, on macOS, in the app menu. The `.deb` package is still updated by installing the new `.deb`.
- Version 0.2.0 cannot update itself, so install 0.2.1 by hand this once. Later versions update themselves.
- Dark mode: choose System, Light or Dark in Options › General › Appearance. System, the default, follows your computer, title bars included. The terminal keeps its own color scheme.
- The "Import connections" window has an "Import from" choice, and besides FinalShell it now reads:
  - Xshell sessions, with their folders. On Windows its Sessions folder is found by itself; to import an `.xts` export, choose the folder that holds it.
  - SecureCRT sessions, with their folders. Its config folder is found where SecureCRT keeps it by default; if you moved it, choose the Config folder, its Sessions folder or any folder inside that.
  - MobaXterm sessions, with their folders, from `MobaXterm.ini` or from an `.mxtsessions` or `.moba` export.
  - OpenSSH's `~/.ssh/config`: each `Host` alias becomes a connection with its `HostName`, `Port`, `User` (your local user name when it has none) and first `IdentityFile`. `Include` is followed, globs included, `Host *` and other patterns apply as they do in `ssh`, and `Match` blocks are skipped. A `ProxyJump` to another host of the file becomes its jump host when that host is imported too or is already in Finallyshell.
  - A server list in a CSV, TSV, text or Excel (`.xlsx`) file, with column headers in English or Chinese. "Save template…" writes a `servers.csv` to fill in. Passwords in the list go to the system keychain, never to `hosts.json`. Old `.xls` workbooks are not read: save them as `.xlsx` or CSV.
- Xshell, SecureCRT and MobaXterm encrypt the passwords they keep, so passwords are not imported: you are asked for each one the first time you connect. A host whose key is kept in the other client's own key store needs its key file chosen after importing.
- In the file panel, right-click the file list or the folder tree to create folders and files, rename (F2) and delete (Delete, or Cmd+Backspace on macOS). A rename never replaces an existing file.
- The README and the documentation are now also in Simplified Chinese.

### 简体中文

- Finallyshell 现在会检查新版本，并直接更新自己。默认自动检查，不需要的话可以在“选项 › 常规”里关掉；“检查更新…”在“选项 › 常规”和托盘图标的菜单里，macOS 上还在应用菜单里。`.deb` 安装包还是要靠安装新的 `.deb` 来更新。
- 0.2.0 自己没法更新，这次请手动安装 0.2.1，以后的版本会自动更新。
- 深色模式：在“选项 › 常规 › 外观”里选“跟随系统”“浅色”或“深色”。默认跟随系统，标题栏也跟着变。终端仍然用它自己的配色。
- “导入连接”窗口多了“导入来源”，除了 FinalShell，现在还能导入：
  - Xshell 的会话，连同文件夹结构。在 Windows 上会自动找到它的 Sessions 文件夹；要导入 `.xts` 导出文件，选它所在的文件夹就行。
  - SecureCRT 的会话，连同文件夹结构。SecureCRT 默认放配置文件夹的地方会自动找；挪过位置的话，选它的 Config 文件夹、里面的 Sessions 文件夹，或者 Sessions 里的任意文件夹。
  - MobaXterm 的会话，连同文件夹结构，从 `MobaXterm.ini`，或者导出的 `.mxtsessions`、`.moba` 文件里读。
  - OpenSSH 的 `~/.ssh/config`：每个 `Host` 别名成为一个连接，带上 `HostName`、`Port`、`User`（没写就用本机的用户名）和第一个 `IdentityFile`。会读取 `Include`（支持通配符），`Host *` 等通配模式的设置和在 `ssh` 里一样生效，`Match` 块会跳过。`ProxyJump` 指向文件里的另一个主机时，只要那个主机也一起导入，或者已经在 Finallyshell 里，就会设成跳板机。
  - CSV、TSV、文本或 Excel（`.xlsx`）文件里的服务器列表，表头写中文或英文都可以。“保存模板…”会导出一个 `servers.csv`，照着填就行。表里的密码存进系统的密钥库，不会写进 `hosts.json`。旧的 `.xls` 文件读不了，请另存为 `.xlsx` 或 CSV。
- Xshell、SecureCRT 和 MobaXterm 都把密码加密保存，所以密码不会导入，第一次连接时会问你。私钥存在这些软件自己密钥库里的主机，导入后要自己选一下私钥文件。
- 文件面板里，在文件列表或目录树上点右键，就能新建文件夹和文件、重命名（F2）、删除（Delete，macOS 上也可以用 Cmd+Backspace）。重命名不会覆盖已有的文件。
- README 和文档有了简体中文版。
