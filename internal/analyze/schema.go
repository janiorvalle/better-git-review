package analyze

import "encoding/json"

var Schema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["title", "overview", "mermaid", "cohorts"],
  "properties": {
    "title": {"type": "string"},
    "overview": {"type": "string"},
    "mermaid": {"type": ["string", "null"]},
    "cohorts": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["title", "layer", "intent", "narrative", "files", "fileSummaries", "reviewNotes", "dependsOn"],
        "properties": {
          "title": {"type": "string"},
          "layer": {"type": "string", "enum": ["schema", "backend", "api", "ui", "tests", "config", "docs", "other"]},
          "intent": {"type": "string"},
          "narrative": {"type": "string"},
          "files": {"type": "array", "items": {"type": "integer", "minimum": 0}},
          "fileSummaries": {"type": "array", "items": {"type": "string"}},
          "reviewNotes": {"type": "array", "items": {"type": "string"}},
          "dependsOn": {"type": "array", "items": {"type": "integer", "minimum": 0}}
        }
      }
    }
  }
}`)
