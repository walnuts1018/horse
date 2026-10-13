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
            "selectors.-moz-range-progress",
            "selectors.-moz-range-thumb",
            "selectors.-moz-range-track",
            "selectors.-webkit-scrollbar",
            "selectors.-webkit-scrollbar-thumb",
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
  },
};
