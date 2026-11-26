import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("spec-diff.py")
PATH = "/projects/{project_id}/products"


def spec(schema):
    return {
        "openapi": "3.1.0",
        "paths": {PATH: {"post": {
            "requestBody": {"content": {"application/json": {"schema": schema}}},
        }}},
    }


class SpecDiffTest(unittest.TestCase):
    def run_diff(self, old, new):
        with tempfile.TemporaryDirectory() as directory:
            old_path = Path(directory, "old.yaml")
            new_path = Path(directory, "new.yaml")
            old_path.write_text(json.dumps(old), encoding="utf-8")
            new_path.write_text(json.dumps(new), encoding="utf-8")
            return subprocess.run(
                [sys.executable, SCRIPT, old_path, new_path],
                capture_output=True,
                text=True,
            )

    def test_landing_spec_is_an_error(self):
        current = spec({"type": "object"})
        result = self.run_diff(current, {"openapi": "3.1.0", "paths": {}})
        self.assertEqual(result.returncode, 2)
        self.assertIn("new.yaml has no OpenAPI operations", result.stderr)
        self.assertEqual(result.stdout, "")

    def test_request_schema_refs_preserve_unchanged_fields(self):
        old = spec({"type": "object", "properties": {"display_name": {"type": "string"}}})
        new = spec({"$ref": "#/components/schemas/CreateProduct"})
        new["components"] = {"schemas": {
            "CreateProduct": {
                "type": "object",
                "properties": {
                    "display_name": {"$ref": "#/components/schemas/Name"},
                    "app_id": {"type": "string"},
                },
                "required": ["app_id"],
            },
            "Name": {"type": "string"},
        }}
        result = self.run_diff(old, new)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("New **required** request body field: `app_id`", result.stdout)
        self.assertNotIn("display_name", result.stdout)

    def test_description_changes_trigger_sync(self):
        old = spec({"type": "object", "description": "Old description"})
        new = spec({"type": "object", "description": "New description"})
        self.assertEqual(self.run_diff(old, old).returncode, 0)
        self.assertEqual(self.run_diff(old, new).returncode, 1)


if __name__ == "__main__":
    unittest.main()
