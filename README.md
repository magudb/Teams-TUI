# teams-tui

[![CI](https://github.com/magudb/Teams-TUI/actions/workflows/ci.yml/badge.svg)](https://github.com/magudb/Teams-TUI/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/magudb/Teams-TUI?include_prereleases)](https://github.com/magudb/Teams-TUI/releases)

A Microsoft Teams client for the terminal, written in Go with [Bubble Tea](https://github.com/charmbracelet/bubbletea).
It talks to Microsoft 365 through the Microsoft Graph API. You can drive it fully from the keyboard or with the mouse.

Download a binary for Linux, macOS or Windows (amd64 and arm64) from [Releases](https://github.com/magudb/Teams-TUI/releases), or build it yourself:

```
go install github.com/magudb/Teams-TUI/cmd/teams-tui@latest   # or: go build -o bin/teams-tui ./cmd/teams-tui
teams-tui --demo      # try it with sample data, no account needed
teams-tui             # sign in with your Microsoft 365 account
```

## Features

| Area | Supported |
|---|---|
| **Chat** | 1:1, group and meeting chats · unread markers and badge · send, edit (↑ on empty box), delete · reply with quote · reactions (any emoji, quick 1–6) · @mentions with autocomplete · importance (important/urgent) · markdown-style formatting, code blocks, emoji shortcodes · attach files (upload to OneDrive / channel SharePoint) · drafts per conversation · new chat (multi-select people) · rename, add people, leave, hide · pin and mute (local) · members, profiles · load older history · copy message or link |
| **Cards** | Adaptive Cards (text, rich text, containers, columns, fact sets, tables, images, inputs), hero/thumbnail, Office 365 connector (GitHub, Azure DevOps, …), list and receipt cards · OpenUrl, ShowCard (expands inline) and ToggleVisibility buttons work, click or press `A` |
| **Teams & channels** | Joined teams, expandable channel tree (standard, private, shared) · posts with subjects · threaded replies (side-by-side on wide terminals) · reply counts and participants · channel Files tab |
| **Activity** | Mentions of you and new incoming messages, with unread state |
| **Presence** | Everyone's presence (polled) · set your own status (Available / Busy / DND / BRB / Away / Offline / reset) · status message |
| **Calendar** | Week strip and day agenda · meeting details, attendees · **Join** (opens the Teams client) · accept / tentative / decline · schedule a new Teams meeting |
| **Files** | Recent files and OneDrive browser · open in browser · download · copy link |
| **Search** | Full-text message search across chats and channels, jump to result |
| **Notifications** | Desktop notifications (macOS / Linux / Windows), optional terminal bell |
| **Alerts** | A dialog for incoming calls, meetings someone has started and meetings about to start, with *Answer / Join in Teams*, *Meeting chat*, *Reply* and *Remind at start* · missed-call dialog with *Call back* · on Linux the desktop notification has a Join / Answer button too |
| **Meetings** | Join banner + `alt+j` · attendees with presence and RSVP · meeting chat · transcripts and recording download after the meeting |
| **Calls** | 📞 / 📹 buttons and **Join** hand the call off to the Teams desktop or web client. A terminal can't carry audio or video. |
| **UI** | Command palette (ctrl+k) · mouse everywhere (click, right-click menus, wheel scroll) · dark/light theme · responsive layout down to 80×24 |

### Not possible through Microsoft Graph or a terminal

- Audio and video calls, screen sharing, whiteboard and live captions. These are handed off to the official client through deep links.
- Typing indicators and read receipts (Graph doesn't expose them).
- Real-time push. Graph change notifications need a public webhook, so the app polls instead: the open conversation every 5 s, the chat list every 15 s, and presence every 60 s. You can change these intervals.
- Unread state for channels isn't exposed by Graph. The app tracks it locally (see below).
- Teams apps, tabs other than Files, and Loop components.
- Bot buttons on cards (`Action.Submit` / `Action.Execute`) and card input fields. These go to the bot through the Bot Framework, which Graph can't call for you, so the app offers to open the message in Teams instead. Inputs are shown read-only.
- Inline images can't be drawn in the terminal. Press `o` on the message to open them.

## Look and feel

The interface follows the *Terminal Chat Client* design (direction "Blocks"):
- **Layout:** panes are separated by fills, not borders. A dark icon rail sits on the left, the sidebar has inverse header bars, and the main pane has a block-filled header.
- **Messages and channels:** chat messages sit on filled blocks, and your own messages are right-aligned in inverse. Channel posts are blocks with a red reply bar that opens the thread.
- **Status bar:** a powerline bar shows your presence, the next meeting and the key hints.
- **Theme:** light paper by default, with a matching dark variant. Switch it in Settings or set `"theme": "dark"`.
- **Icons:** Nerd Font glyphs, as in the design. Use a [Nerd Font](https://www.nerdfonts.com) in your terminal (any `… Nerd Font Mono`), or set `"icons": "ascii"`, or switch in Settings.

Screens: sign-in, Activity, Chat, Teams and channels with threads, adaptive cards, Calendar, the live meeting view, Files, Search, the command palette (ctrl+k) and Settings (the gear in the rail). The layout adapts down to 80×24: a narrow rail, a shorter sidebar, and a thread that replaces the channel instead of opening beside it.

## Combined Chat tab

By default the Chat tab shows everything in one list, like the "Chats and channels" view in new Teams:

1. **Unread**: chats and channels with new messages.
2. **Channels**: your teams and their channels.
3. **Chats**: pinned chats, then recent ones. Choose **Load older chats** at the end of the list to fetch another page.

Each section header can be collapsed. The separate Teams tab is hidden in this mode; set `"combined_chat": false` to get it back.

Graph has no unread state for channels, so the app tracks it itself. In the background it checks one channel at a time (every `poll_channel_seconds`, default 4) for the newest post or reply. It compares that with when you last opened the channel, which is saved in `state.json`. Channels you've never opened start as read. With many channels, a new post can take a while to show up: with 40 channels it's about 2½ minutes. Reactions don't mark a channel as unread. Muted channels (`m`) stay out of Unread. You get a desktop notification when someone @mentions you in a channel.

## Meetings

The terminal works as a meeting companion. Audio and video stay in the Teams app.

- **Join**: the status bar shows the meeting that's live or starts within 10 minutes, with a *Join* button (or press `alt+j`). Joining opens the meeting in the Teams client.
- **Alerts**: a dialog pops up when a meeting is about to start (`meeting_reminder_minutes` before), when someone starts a meeting you're invited to, and when someone calls you. Pick an action with `←→` and `enter`, its letter, or the mouse; `esc` dismisses. See [Call and meeting alerts](#call-and-meeting-alerts).
- **Calendar details**: attendees with their presence and RSVP (✓ ~ ✗ ?), plus buttons for the meeting chat, accept/maybe/decline, and Outlook.
- **Meeting chat**: press `c` in the calendar, or *Join* in the header of a meeting chat.
- **After the meeting**: open the transcript (`x`, with copy and save to Downloads) and download the recording (`R`). Graph normally only gives these to the meeting organizer, and an admin can turn API access to them off. The app tells you when that's the case.

These features need three extra scopes: `OnlineMeetings.Read`, `OnlineMeetingTranscript.Read.All` and `OnlineMeetingRecording.Read.All`. If you signed in before they were added, run `teams-tui login` again. Existing sessions keep working without them.

## Call and meeting alerts

| Alert | When | Actions |
|---|---|---|
| **Meeting starting soon** | `meeting_reminder_minutes` before a meeting in your calendar (not all-day, cancelled or declined ones) | Join in Teams · Meeting chat · Remind at start |
| **Meeting started** | someone starts a meeting whose chat you're in | Join in Teams · Meeting chat |
| **Incoming call** | someone calls you or starts a call in a group chat | Answer in Teams (opens the chat, where Teams shows the call) · Reply with a message |
| **Missed call** | a 1:1 call ended without you | Call back in Teams · Send a message |

The dialog never interrupts typing. While you're writing a message or have another dialog open, the alert waits in the status bar and opens after a 3 second pause, or at once with `alt+a`. Clicking outside the dialog sends it back to the status bar. On Linux the desktop notification carries the main action as a button (needs `notify-send` 0.8 or newer), so you can join from another window.

Graph has no ringing signal. Calls and started meetings are detected from the event Teams posts to the chat, which the chat list poll picks up every `poll_chats_seconds` (default 15). So an alert can be that late, and a short ring may only show up as a missed call. Lower `poll_chats_seconds` (minimum 5) to react faster. Nothing alerts while your presence says you're already on a call. Set `"alerts": false`, or switch it off in Settings, to get a plain notification instead of a dialog.

## List order

The lists follow the same rules as the Teams desktop app:

- **Chats**: *Pinned* first, in your own order, then *Recent*, newest activity first.
- **Teams**: the order Microsoft Graph returns, with your own reordering applied on top. Hidden teams collapse into a *Hidden teams* group at the bottom.
- **Channels**: the channel named *General* first, then all others alphabetically. Sorting uses your locale (`LANG`), so Danish æ, ø and å come after z.

To reorder, drag with the mouse, use `alt+↑` / `alt+↓` (or `K` / `J`), or pick *Move up* / *Move down* in the `.` actions menu. Press `H` to hide or show a team.

Graph doesn't expose the pins, custom team order or hidden teams you set in the Teams app. So this order is stored locally in `state.json` and doesn't sync with the official client.

## Signing in

On the first run the app shows a sign-in screen with a **device code**: `o` opens `microsoft.com/devicelogin`, `c` copies the code, `r` requests a fresh code, `↑↓` picks a tenant, and `d` starts demo mode instead. The token is cached in `~/.config/teams-tui/token.json` (mode 0600), and access tokens refresh on their own. The cache records the tenant authority and client ID used at sign-in. Changing either, or upgrading from a cache that predates these fields, requires signing in again.

By default the app uses Microsoft's public *Microsoft Graph Command Line Tools* client id. Many tenants block that client or require admin consent for it. For an organisation it's better to register your own app:

1. Azure portal → **Microsoft Entra ID** → **App registrations** → **New registration**. Choose single tenant. You don't need a redirect URI.
2. **Authentication** → *Advanced settings* → **Allow public client flows: Yes**.
3. **API permissions** → *Microsoft Graph* → **Delegated**, then add:
   `User.Read, User.ReadBasic.All, People.Read, Presence.Read.All, Presence.ReadWrite, Chat.ReadWrite, ChatMember.ReadWrite, ChatMessage.Send, Team.ReadBasic.All, Channel.ReadBasic.All, ChannelMember.Read.All, ChannelMessage.Read.All, ChannelMessage.Send, ChannelMessage.ReadWrite, Calendars.ReadWrite, Files.ReadWrite.All, Sites.Read.All, OnlineMeetings.Read, OnlineMeetingTranscript.Read.All, OnlineMeetingRecording.Read.All, offline_access`
4. **Grant admin consent**. `ChannelMessage.Read.All` and `ChannelMessage.ReadWrite` always need it.
5. Put the ids in `~/.config/teams-tui/config.json`:

```json
{ "client_id": "<application id>", "tenant": "<tenant id or domain>" }
```

You can also pass them as flags (`--client-id`, `--tenant`) or environment variables (`TEAMS_TUI_CLIENT_ID`, `TEAMS_TUI_TENANT`).
Other commands: `teams-tui login`, `teams-tui logout`, `teams-tui config` (prints the config path).

## Configuration

`~/.config/teams-tui/config.json` is created with defaults on the first run:

| Key | Default | |
|---|---|---|
| `desktop_notifications` | `true` | native notifications for new messages |
| `alerts` | `true` | dialogs for incoming calls and starting meetings |
| `bell` | `false` | ring the terminal bell |
| `poll_messages_seconds` / `poll_chats_seconds` / `poll_presence_seconds` | 5 / 15 / 60 | how often to poll |
| `download_dir` | `~/Downloads` | |
| `time_format` / `date_format` | `15:04` / `Mon 02 Jan` | Go time layouts |
| `sidebar_width` | 33 | |
| `theme` | `light` | or `dark` |
| `icons` | `nerd` | Nerd Font glyphs, or `ascii` if your terminal font has no Nerd Font symbols |
| `meeting_reminder_minutes` | 5 | reminder before meetings |
| `combined_chat` | `true` | channels and chats in one Chat tab (Unread → Channels → Chats) |
| `poll_channel_seconds` | 4 | pause between background channel checks for unread tracking (0 = off) |

Local UI state (pinned chats and their order, team order, hidden teams, muted chats, expanded teams, drafts) is kept in `state.json` next to the config.

## Keys

Press `?` in the app for the full list. The essentials:

| | |
|---|---|
| `alt+1…6` | Activity · Chat · Teams · Calendar · Files · Search |
| `tab` / `shift+tab` | cycle focus: sidebar → messages → compose |
| `ctrl+k` | command palette: jump anywhere, run any action |
| `ctrl+n` | new chat |
| `ctrl+e` or `/` | search |
| `enter` | open; in compose: send (`alt+enter` / `ctrl+j` for a newline) |
| `r` `+` `1-6` `e` `d` `y` `o` `.` | reply · react · quick-react · edit · delete · copy · open links · more |
| `esc` | back |

In compose you can type `/attach <path>`, `/subject <text>`, `/important`, `/urgent`, `/status <text>`, `/busy`, `/dnd`, `/meeting <title>`, `/rename <name>`, and `/help`.

## Development

```
go test ./...          # unit tests + headless UI tests against the demo backend
go run ./cmd/teams-tui --demo
```

Layout:

- `internal/model`: domain types and the `Service` interface.
- `internal/graph`: the Microsoft Graph implementation.
- `internal/demo`: in-memory sample data.
- `internal/auth`: device-code OAuth.
- `internal/htmltext`: Teams HTML ⇄ styled terminal text and light markdown.
- `internal/ui`: the Bubble Tea app. It uses bubblezone for mouse hit-testing.

### CI and releases

- **CI** (`.github/workflows/ci.yml`) runs on every push to `main` and on pull requests. It checks gofmt, `go vet` and `go mod tidy`. It runs the tests on Linux (with `-race`), macOS and Windows. It also builds every platform with GoReleaser and attaches the snapshot archives to the run.
- **Releases** (`.github/workflows/release.yml`) run when you push a version tag. GoReleaser (`.goreleaser.yaml`) builds Linux, macOS and Windows binaries for amd64 and arm64 and publishes them with a changelog and `checksums.txt`:

  ```
  git tag -a v0.1.0 -m "v0.1.0"
  git push origin v0.1.0
  ```

  Tags with a suffix (for example `v0.2.0-rc.1`) are published as pre-releases. The macOS binaries aren't signed or notarized.
