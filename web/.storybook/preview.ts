import type { Preview } from "@storybook/react-vite";

import "../src/app/styles/global.css";

const preview: Preview = {
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/iu,
        date: /Date$/u,
      },
    },
  },
};

export default preview;
