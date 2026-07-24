import { useEffect, useRef } from "react";
import * as monaco from "monaco-editor";
import EditorWorker from "monaco-editor/editor/editor.worker?worker";
import JSONWorker from "monaco-editor/language/json/json.worker?worker";
import TSWorker from "monaco-editor/language/typescript/ts.worker?worker";
import { configureMonacoYaml, type JSONSchema } from "monaco-yaml";
import mihomoSchema from "meta-json-schema/schemas/meta-json-schema.json";
import YAMLWorker from "./mihomo-yaml.worker?worker";

type RewriteKind = "yaml" | "javascript";

const environment = globalThis as typeof globalThis & {
  MonacoEnvironment?: { getWorker: (_moduleId: string, label: string) => Worker };
};

environment.MonacoEnvironment = {
  getWorker: (_moduleId, label) => {
    if (label === "yaml") return new YAMLWorker();
    if (label === "json") return new JSONWorker();
    if (label === "javascript" || label === "typescript") return new TSWorker();
    return new EditorWorker();
  }
};

const clashPartySchema = {
  ...mihomoSchema,
  title: "Mihomo config rewrite (Clash Party semantics)",
  patternProperties: {
    "^\\+.+$": { description: "Prepend to an array (Clash Party +key syntax)" },
    ".+\\+$": { description: "Append to an array (Clash Party key+ syntax)" },
    ".+!$": { description: "Force-replace a value (Clash Party key! syntax)" }
  }
};

configureMonacoYaml(monaco, {
  enableSchemaRequest: false,
  validate: true,
  format: { enable: true },
  hover: true,
  completion: true,
  yamlVersion: "1.2",
  schemas: [
    {
      uri: "boxfleet://schemas/mihomo-rewrite.json",
      fileMatch: ["**/*.boxfleet-rewrite.yaml"],
      schema: clashPartySchema as unknown as JSONSchema
    }
  ]
});

const javascriptDefaults = (monaco as unknown as {
  typescript: { javascriptDefaults: { addExtraLib: (source: string, path: string) => unknown } };
}).typescript.javascriptDefaults;

javascriptDefaults.addExtraLib(
  `type MihomoProxy = Record<string, unknown> & { name: string; type: string };
interface MihomoConfig {
  proxies?: MihomoProxy[];
  "proxy-groups"?: Array<Record<string, unknown> & { name: string; type: string }>;
  rules?: string[];
  [key: string]: unknown;
}
/** Return the rewritten config synchronously. Node APIs and async results are unavailable. */
declare function main(config: MihomoConfig): MihomoConfig;`,
  "boxfleet://types/mihomo-rewrite.d.ts"
);

const editorOptions: monaco.editor.IStandaloneEditorConstructionOptions = {
  automaticLayout: true,
  minimap: { enabled: false },
  fontSize: 13,
  lineHeight: 20,
  scrollBeyondLastLine: false,
  wordWrap: "on",
  padding: { top: 12 },
  theme: window.matchMedia("(prefers-color-scheme: dark)").matches ? "vs-dark" : "vs"
};

export function MihomoCodeEditor({
  value,
  kind,
  readOnly = false,
  onChange
}: {
  value: string;
  kind: RewriteKind;
  readOnly?: boolean;
  onChange?: (value: string) => void;
}) {
  const hostRef = useRef<HTMLDivElement>(null);
  const editorRef = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const changeRef = useRef(onChange);
  const valueRef = useRef(value);

  useEffect(() => {
    changeRef.current = onChange;
    valueRef.current = value;
  }, [onChange, value]);

  useEffect(() => {
    const colorScheme = window.matchMedia("(prefers-color-scheme: dark)");
    const syncTheme = () => monaco.editor.setTheme(colorScheme.matches ? "vs-dark" : "vs");
    syncTheme();
    colorScheme.addEventListener("change", syncTheme);
    return () => colorScheme.removeEventListener("change", syncTheme);
  }, []);

  useEffect(() => {
    if (!hostRef.current) return;
    const uri = monaco.Uri.parse(
      kind === "yaml" ? `file:///rewrite-${crypto.randomUUID()}.boxfleet-rewrite.yaml` : `file:///rewrite-${crypto.randomUUID()}.js`
    );
    const model = monaco.editor.createModel(valueRef.current, kind === "yaml" ? "yaml" : "javascript", uri);
    const editor = monaco.editor.create(hostRef.current, { ...editorOptions, model, readOnly });
    editorRef.current = editor;
    const subscription = editor.onDidChangeModelContent(() => changeRef.current?.(editor.getValue()));
    return () => {
      subscription.dispose();
      editor.dispose();
      model.dispose();
      editorRef.current = null;
    };
  }, [kind, readOnly]);

  useEffect(() => {
    const editor = editorRef.current;
    if (editor && editor.getValue() !== value) editor.setValue(value);
  }, [value]);

  return <div ref={hostRef} className="h-[34rem] overflow-hidden rounded-lg border border-kumo-line" />;
}
