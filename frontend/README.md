# React + TypeScript + Vite

This template provides a minimal setup to get React working in Vite with HMR and some ESLint rules.

Currently, two official plugins are available:

- [@vitejs/plugin-react](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react) uses [Babel](https://babeljs.io/) (or [oxc](https://oxc.rs) when used in [rolldown-vite](https://vite.dev/guide/rolldown)) for Fast Refresh
- [@vitejs/plugin-react-swc](https://github.com/vitejs/vite-plugin-react/blob/main/packages/plugin-react-swc) uses [SWC](https://swc.rs/) for Fast Refresh

## React Compiler

The React Compiler is not enabled on this template because of its impact on dev & build performances. To add it, see [this documentation](https://react.dev/learn/react-compiler/installation).

## Expanding the ESLint configuration

If you are developing a production application, we recommend updating the configuration to enable type-aware lint rules:

```js
export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...

      // Remove tseslint.configs.recommended and replace with this
      tseslint.configs.recommendedTypeChecked,
      // Alternatively, use this for stricter rules
      tseslint.configs.strictTypeChecked,
      // Optionally, add this for stylistic rules
      tseslint.configs.stylisticTypeChecked,

      // Other configs...
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

You can also install [eslint-plugin-react-x](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-x) and [eslint-plugin-react-dom](https://github.com/Rel1cx/eslint-react/tree/main/packages/plugins/eslint-plugin-react-dom) for React-specific lint rules:

```js
// eslint.config.js
import reactX from 'eslint-plugin-react-x'
import reactDom from 'eslint-plugin-react-dom'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      // Other configs...
      // Enable lint rules for React
      reactX.configs['recommended-typescript'],
      // Enable lint rules for React DOM
      reactDom.configs.recommended,
    ],
    languageOptions: {
      parserOptions: {
        project: ['./tsconfig.node.json', './tsconfig.app.json'],
        tsconfigRootDir: import.meta.dirname,
      },
      // other options...
    },
  },
])
```

## Frontend interaction and styling

- The workspace keeps the existing graphite palette, cobalt accent, fonts, and light/dark themes. Shared tokens and responsive workspace styles live in `src/index.css`.
- At 1280px and above, navigation uses the full sidebar. Between 768px and 1279px, an icon rail leaves more room for mail. Below 1024px, opening a message switches from the list to the reader; use **返回** or **Esc** to return.
- Search supports `/`; `j` and `k` move between messages; `?` opens the shortcut reference. Navigation and reading are keyboard accessible, with visible focus across controls. Mail is read-only: there are no delete, read/unread, or batch mutation controls.
- Hover tooltips flip above or below their trigger to fit the viewport, keep an 8px edge inset, and wrap long content on narrow screens.
- Desktop inputs and primary controls are 36px high. Touch and phone layouts provide 44px action targets. Mobile navigation has three equal columns and respects device safe areas.
- Refresh buttons keep their width and show a readable busy state with a continuously rotating loading icon; reduced-motion mode slows the rotation. The inbox header and account row announce progress while existing messages remain readable; an empty inbox shows a loading state until refresh completes.
- Account form labels target their inputs; provider and color selectors expose their selected state. The account drawer contains keyboard focus and restores it when closed.
- Each account caches its latest 50 inbox messages, and the combined list shows the latest 50 across accounts. Opening a message loads its body on demand without changing its remote read state.
- Account reads distinguish loading, failure, and an empty result, with cancellation and request ordering to ignore stale responses. Failed reads provide a retry without repeating successful mutations.
- Account submissions and deletions have explicit pending states; concurrent refreshes track each account independently. A server sync followed by a failed list read reports the two outcomes separately and keeps the current messages readable.
- `MailProvider` owns both `MailStore` and `AccountStore` for one authenticated session. Routes reuse loaded accounts/providers; mutations invalidate shared state, cancelled reads restart on the next subscriber, and session changes dispose pending work.
- Provider options come from `/api/providers`. Custom accounts expose labelled IMAP host and port fields for both creation and editing. Endpoint changes invalidate old message bodies; initial-sync failures remain distinct from a successfully saved account.
- The latest-50 list renders ordinary rows. `MailList`, `MailReader`, inbox actions, and keyboard handling are separate from the page composition.
- The body cache uses recent-access eviction, a 100-message ceiling, and a 16 MiB budget for UTF-16 body strings. A body larger than 2 MiB remains available only as the current reader detail; these limits are not a total browser heap measurement.

The refinement follows [unslop](https://heydamir.design/unslop) by Damir Sokolovsky ([@heydamir](https://x.com/heydamir)), licensed under CC BY 4.0.
