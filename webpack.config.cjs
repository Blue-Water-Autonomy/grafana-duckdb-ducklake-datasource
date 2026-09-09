const path = require("node:path");
const CopyPlugin = require("copy-webpack-plugin");
const pkg = require("./package.json");

module.exports = (_, argv) => ({
  entry: "./src/module.ts",
  devtool: "source-map",
  output: {
    path: path.resolve(__dirname, "dist"),
    filename: "module.js",
    library: { type: "amd" },
    publicPath: "public/plugins/local-duckdb-datasource/",
    uniqueName: "local-duckdb-datasource",
    clean: { keep: /gpx_local_duckdb_/ },
  },
  externals: [
    "react",
    "react-dom",
    "rxjs",
    "@grafana/data",
    "@grafana/runtime",
    "@grafana/ui",
  ],
  resolve: { extensions: [".ts", ".tsx", ".js"] },
  module: {
    rules: [
      {
        test: /\.tsx?$/,
        exclude: /node_modules/,
        loader: "esbuild-loader",
        options: { target: "es2020" },
      },
    ],
  },
  plugins: [
    new CopyPlugin({
      patterns: [
        {
          from: "src/plugin.json",
          to: "plugin.json",
          transform(content) {
            return JSON.stringify(
              {
                ...JSON.parse(content),
                buildMode: argv.mode,
                info: {
                  ...JSON.parse(content).info,
                  version: pkg.version,
                  updated: new Date().toISOString().slice(0, 10),
                },
              },
              null,
              2,
            );
          },
        },
        { from: "src/img", to: "img" },
        { from: "README.md" },
        { from: "LICENSE" },
        { from: "docs", to: "docs" },
      ],
    }),
  ],
});
