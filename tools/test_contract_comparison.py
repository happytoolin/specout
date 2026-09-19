"""Keep the contract comparison from hiding upstream constraints."""
import unittest

from check_compatibility import canonical_schema, dereference


class ContractComparisonTest(unittest.TestCase):
    def test_constraints_survive_normalization(self):
        schema = {
            "type": "array", "description": "annotation",
            "prefixItems": [{"type": "string", "minLength": 2}],
            "contains": {"type": "integer"}, "minContains": 1,
            "unevaluatedItems": False,
        }
        expected = {key: value for key, value in schema.items() if key != "description"}
        self.assertEqual(canonical_schema(schema), expected)
        self.assertNotEqual(canonical_schema(schema), canonical_schema({"type": "array"}))

    def test_nullable_source_and_property_names(self):
        self.assertEqual(
            canonical_schema({"type": "string", "nullable": True}),
            {"type": ["null", "string"]},
        )
        schema = {"type": "object", "properties": {"x-field": {"type": "boolean"}}}
        self.assertEqual(canonical_schema(schema), schema)

    def test_unsupported_composition_and_recursive_refs_fail(self):
        with self.assertRaises(ValueError):
            canonical_schema({"allOf": [{"type": "string"}, {"type": "integer"}]})
        with self.assertRaises(ValueError):
            dereference({"$ref": "#/self"}, {"self": {"$ref": "#/self"}})


if __name__ == "__main__":
    unittest.main()
