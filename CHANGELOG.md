# Changelog

Each version's notes, newest first, in English and Simplified Chinese. A release's notes on GitHub and in the app's update window are its section here; see [Releasing](docs/en/development.md#releasing).

## 0.2.1

### English

- Finallyshell now checks for new versions and updates itself in place. "Check for Updates…" is in Options › General, in the tray icon's menu and, on macOS, in the app menu, and automatic checks can be turned off in Options › General. The `.deb` package is still updated by installing the new `.deb`.
- Version 0.2.0 cannot update itself, so install 0.2.1 by hand this once. Later versions update themselves.
- Dark mode: choose System, Light or Dark in Options › General › Appearance. System, the default, follows your computer, title bars included. The terminal keeps its own color scheme.
- The "Import connections" window has an "Import from" choice, and besides FinalShell it now reads:
  - OpenSSH's `~/.ssh/config`: each host alias becomes a connection with its host name, port, user, key file and jump host. `Include` is followed, and `Host *` settings apply as they do in `ssh`.
  - A server list in a CSV or Excel (`.xlsx`) file, with column headers in English or Chinese. Passwords in the list go to the system keychain, never to `hosts.json`, and "Save template…" writes a `servers.csv` to fill in.
  - Sessions from Xshell (its Sessions folder or an `.xts` export), SecureCRT (its Sessions folder) and MobaXterm (`MobaXterm.ini` or an `.mxtsessions` export), with their folders. These clients encrypt the passwords they keep, so passwords are not imported: enter them when you first connect.
- In the file panel, right-click the file list or the folder tree to create folders and files, rename (F2) and delete (Delete, or Cmd+Backspace on macOS). A rename never replaces an existing file.
- The README and the documentation are now also in Simplified Chinese.

### 简体中文

- Finallyshell 现在会检查新版本，并直接更新自己。“检查更新…”在“选项 › 常规”和托盘图标的菜单里，macOS 上还在应用菜单里；不想自动检查，可以在“选项 › 常规”里关掉。`.deb` 安装包还是要靠安装新的 `.deb` 来更新。
- 0.2.0 自己没法更新，这次请手动安装 0.2.1，以后的版本会自动更新。
- 深色模式：在“选项 › 常规 › 外观”里选“跟随系统”“浅色”或“深色”。默认跟随系统，标题栏也跟着变。终端仍然用它自己的配色。
- “导入连接”窗口多了“导入来源”，除了 FinalShell，现在还能导入：
  - OpenSSH 的 `~/.ssh/config`：每个 Host 别名成为一个连接，带上主机名、端口、用户、私钥和跳板机。会读取 `Include`，`Host *` 里的设置和 `ssh` 里一样生效。
  - CSV 或 Excel（`.xlsx`）格式的服务器列表，表头写中文或英文都可以。表里的密码存进系统钥匙串，不会写进 `hosts.json`；“保存模板…”会导出一个 `servers.csv`，照着填就行。
  - Xshell（会话文件夹或 `.xts` 导出文件）、SecureCRT（会话文件夹）和 MobaXterm（`MobaXterm.ini` 或 `.mxtsessions` 导出文件）的会话，文件夹结构也一起导入。这几个软件都把密码加密保存，所以密码不会导入，第一次连接时输入就好。
- 文件面板里，在文件列表或目录树上点右键，就能新建文件夹和文件、重命名（F2）、删除（Delete，macOS 上也可以用 Cmd+Backspace）。重命名不会覆盖已有的文件。
- README 和文档有了简体中文版。
