export default {
  plugins: ["stylelint-browser-compat"],
  extends: ["stylelint-config-standard"],
  rules: {
    "plugin/browser-compat": [
      true,
      {
        allow: {
          features: [
            "properties.scrollbar-color",
            "properties.accent-color",
            "properties.-webkit-tap-highlight-color",
            "selectors.-moz-range-progress",
            "selectors.-moz-range-thumb",
            "selectors.-moz-range-track",
            "selectors.-webkit-scrollbar",
            "selectors.-webkit-scrollbar-thumb",
            "selectors.-webkit-progress-bar",
            "selectors.-webkit-progress-value",
            "selectors.-webkit-slider-runnable-track",
            "selectors.-webkit-slider-thumb",
          ],
        },
      },
    ],
    "color-function-notation": null,
    "alpha-value-notation": null,
    "selector-class-pattern": null,
    "declaration-empty-line-before": null,
    "no-descending-specificity": null,
    "import-notation": "string",
    "lightness-notation": "number",
    "hue-degree-notation": "number",
    "alpha-value-notation": "number",
    "color-function-notation": "modern",
    "custom-property-pattern": [
      "^([a-z][a-z0-9]*)(-[a-z0-9]+)*(-\\*)?$",
      {
        message: "Expected custom property name to be kebab-case",
      },
    ],
  },
};
