# One-Mail Frontend

React 19, TypeScript, Vite and TailwindCSS 4. Use Node.js 24.22.3 and npm 11.19.1, pinned by `.node-version`, `package.json` and the repository's `mise.toml`.

```bash
npm ci
npm run dev
npm test
npm run lint
npm run build
npm audit --audit-level=moderate
```

Vitest runs `tests/**/*.test.{ts,tsx}` in jsdom with Testing Library. `npm test` runs once without Web Storage environment flags. API requests use same-origin `/api`, proxied to `http://127.0.0.1:8080` in development; set `VITE_API_BASE_URL` for a separate API origin.

Run both services with `make run` from the repository root. The supervisor supports macOS/Linux/WSL and stops both process groups on exit or interruption. `make test-dev` verifies cleanup with isolated fixture processes. See [performance verification](../docs/performance.md) for backend synthetic benchmarks and limits.

## Interaction and styling

- The graphite palette, cobalt accent, fonts and light/dark themes use shared tokens in `src/index.css`. Full navigation starts at 1280px, an icon rail covers 768–1279px, and mobile navigation has three columns with safe-area support. Opening a message below 1024px switches to the reader; **返回** or **Esc** returns to the list.
- `/` focuses search, `j`/`k` changes messages, single-key `r` refreshes, and `?` opens shortcuts. Meta/Ctrl/Alt combinations retain browser behavior, including page reload. Visible focus supports keyboard navigation.
- Tooltips flip vertically, keep an 8px viewport inset and wrap long text. Desktop inputs and controls are 36px high; touch and mobile action targets are 44px.
- Refresh controls keep their width and announce progress while existing mail remains readable. Loading icons respect reduced motion. Account forms have labelled inputs, initial focus, a focus trap and focus restoration.
- Mail is read-only. Lists offer no delete, read/unread or batch mutation controls, and loading a body does not alter remote read state.

## Mail, accounts and lifecycle

- **设置 → 邮件阅读 → 默认加载邮件数** offers 25, 50 (default), 100 or 200 messages. The browser remembers this preference. List and refresh use the same limit; each account keeps its requested window and the combined list shows at most that many newest messages. Increasing the limit requires refresh.
- The unified inbox sorts by date and identifies messages with the full account address. The account selector remembers its selection, including all accounts, across reloads and route changes. Loading or failed account reads preserve the selection; successful reads clear a selection only after confirming that account no longer exists.
- Remote images load by default. The saved blocking preference can stop them; temporary permission applies to the current message. HTML is sanitized inside an opaque iframe sandbox with no-referrer and resource CSP. Theme changes reuse sanitized HTML; changes to content or image permission sanitize again.
- `MailProvider` owns session-scoped `MailStore` and `AccountStore`. Routes reuse account/provider reads; mutations invalidate state. Query cancellation and generation checks reject stale responses, and ending a session disposes pending work and cached mail.
- List and detail use separate types. Account/search changes close the current reader. The body cache uses recent-access eviction, a 100-message ceiling and a 16 MiB UTF-16 string budget; a body larger than 2 MiB remains only in the current reader. These are not total browser heap limits.
- Account reads distinguish loading, failure and empty results. Submissions, deletion and per-account connection tests prevent duplicate actions, ignore results after unmount and retain retry after failures. A successful remote refresh followed by a failed list read reports both outcomes and retries only the list.
- Providers come from `/api/providers`. Complete case-insensitive domains match Gmail/Googlemail, QQ/Foxmail, iCloud/me/mac, Outlook/Hotmail/Live/MSN and 163. Unknown domains expose IMAP host and port with TLS guidance; editing an unchanged email preserves saved endpoint settings.
- New accounts use the trimmed email as login username; changing the email updates the username, while unrelated edits preserve it. iCloud shows the App-specific password and two-factor requirements in both create and edit forms.
- Account creation has a 35-second client timeout around the server's 25-second budget. A saved account and failed initial sync are distinct outcomes. Repeated mailbox creation is rejected by the server; reload accounts or edit the existing entry.
- Telegram notifications are discovered by manual mailbox refresh. Settings shows pending/failed counts and the latest 50 failures, with retry and cleanup for failed tasks. Successful tasks disappear after acknowledgement; task lists never expose message snapshots.

The refinement follows [unslop](https://heydamir.design/unslop) by Damir Sokolovsky ([@heydamir](https://x.com/heydamir)), licensed under CC BY 4.0.
