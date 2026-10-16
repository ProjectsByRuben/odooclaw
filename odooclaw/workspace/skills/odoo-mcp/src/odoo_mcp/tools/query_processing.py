"""Bounded read-only processing; every data fetch uses delegated native Odoo ACLs."""
import unicodedata
from random import SystemRandom
from odoo_mcp.core.domains import validate_domain
from odoo_mcp.security.guards import guard_model_access
from odoo_mcp.tools.records import _validate_fields
from odoo_mcp.core.serializers import serialize_records


def count_vowels(text):
    normalized = unicodedata.normalize("NFD", text.casefold())
    return sum(char in "aeiou" for char in normalized)


def _validate(client, user_id, model, domain, fields, limit):
    if type(user_id) is not int or user_id <= 1:
        raise ValueError("Authenticated internal user required")
    if type(limit) is not int or not 1 <= limit <= 200:
        raise ValueError("Limit must be 1-200")
    if not isinstance(fields, list) or not fields or len(fields) > 25:
        raise ValueError("Specify 1-25 fields")
    validate_domain(domain)
    guard_model_access(model, client, sender_id=user_id)
    return _validate_fields(client, model, fields, user_id)


def query_records(client, user_id, model, domain, fields, conditions,
                  limit=100, after_id=0, scan_limit=20000):
    clean = _validate(client, user_id, model, domain, fields, limit)
    if not isinstance(conditions, list) or not 1 <= len(conditions) <= 12:
        raise ValueError("Specify 1-12 text conditions")
    text_fields = []
    for condition in conditions:
        if not isinstance(condition, dict) or set(condition) - {"field", "operation", "value", "comparison", "case_sensitive", "ignore_accents"}:
            raise ValueError("Invalid condition keys")
        field = condition.get("field")
        operation = condition.get("operation")
        value = condition.get("value")
        if not isinstance(field, str) or not field:
            raise ValueError("Each condition needs a field")
        if operation in ("vowel_count", "length", "word_count"):
            if type(value) is not int or not 0 <= value <= 1000000:
                raise ValueError("Computed text conditions need a non-negative integer")
        elif operation in ("contains", "not_contains", "starts_with", "ends_with", "equals"):
            if not isinstance(value, str) or len(value) > 500:
                raise ValueError("Text conditions need a string of at most 500 characters")
        else:
            raise ValueError("Unsupported operation; Python, SQL and regex are not accepted")
        if condition.get("comparison", "eq") not in ("eq", "ne", "gt", "gte", "lt", "lte"):
            raise ValueError("Unsupported comparison")
        for flag in ("case_sensitive", "ignore_accents"):
            if flag in condition and type(condition[flag]) is not bool:
                raise ValueError("Condition flags must be booleans")
        text_fields.append(field)
    if type(after_id) is not int or after_id < 0:
        raise ValueError("after_id must be a non-negative record ID")
    if type(scan_limit) is not int or not 1 <= scan_limit <= 20000:
        raise ValueError("scan_limit must be 1-20000")
    schema = client.call_kw(model, "fields_get", args=[list(dict.fromkeys(text_fields))],
                           kwargs={"attributes": ["type"]}, sender_id=user_id)
    if any(schema.get(field, {}).get("type") not in ("char", "text") for field in text_fields):
        raise ValueError("Filter fields must be accessible char/text fields")
    read_fields = list(dict.fromkeys(["id", *text_fields, *clean]))
    matched, scanned, cursor = [], 0, after_id
    complete = False
    while scanned < scan_limit:
        page_size = min(500, scan_limit - scanned)
        rows = client.call_kw(model, "search_read", args=[domain + [["id", ">", cursor]]],
                             kwargs={"fields": read_fields, "limit": page_size, "order": "id asc"},
                             sender_id=user_id)
        if not rows:
            complete = True
            break
        for row in rows:
            cursor = row["id"]
            scanned += 1
            if all(_matches(row.get(condition["field"]) or "", condition) for condition in conditions):
                matched.append({field: row.get(field) for field in dict.fromkeys(["id", *clean])})
                if len(matched) >= limit:
                    return {"records": serialize_records(matched, model, client.odoo_session.url),
                            "scanned": scanned, "complete": False, "next_after_id": cursor,
                            "conditions": conditions}
        if len(rows) < page_size:
            complete = True
            break
    return {"records": serialize_records(matched, model, client.odoo_session.url),
            "scanned": scanned, "complete": complete,
            "next_after_id": None if complete else cursor,
            "conditions": conditions}



def _matches(text, condition):
    if not isinstance(text, str):
        return False
    operation = condition["operation"]
    value = condition["value"]
    if operation in ("vowel_count", "length", "word_count"):
        actual = {"vowel_count": count_vowels, "length": len,
                  "word_count": lambda v: len(v.split())}[operation](text)
        comparison = condition.get("comparison", "eq")
        return {"eq": actual == value, "ne": actual != value, "gt": actual > value,
                "gte": actual >= value, "lt": actual < value, "lte": actual <= value}[comparison]
    def normalize(v):
        if not condition.get("case_sensitive", False):
            v = v.casefold()
        if condition.get("ignore_accents", True):
            v = "".join(c for c in unicodedata.normalize("NFD", v) if not unicodedata.combining(c))
        return v
    text, value = normalize(text), normalize(value)
    return {"contains": value in text, "not_contains": value not in text,
            "starts_with": text.startswith(value), "ends_with": text.endswith(value),
            "equals": text == value}[operation]


def filter_text(client, user_id, model, domain, fields, text_field, vowel_count,
                limit=100, after_id=0, scan_limit=20000):
    return query_records(client, user_id, model, domain, fields,
                         [{"field": text_field, "operation": "vowel_count", "value": vowel_count}],
                         limit, after_id, scan_limit)


def sample_records(client, user_id, model, domain, fields, limit=3):
    clean = _validate(client, user_id, model, domain, fields, limit)
    if limit > 20:
        raise ValueError("Random sample limit must be 1-20")
    count = client.call_kw(model, "search_count", args=[domain], kwargs={}, sender_id=user_id)
    positions = SystemRandom().sample(range(count), min(limit, count))
    selected = []
    for position in positions:
        ids = client.call_kw(model, "search", args=[domain],
                             kwargs={"order": "id asc", "offset": position, "limit": 1}, sender_id=user_id)
        selected.extend(record_id for record_id in ids if record_id not in selected)
    rows = client.call_kw(model, "read", args=[selected], kwargs={"fields": clean}, sender_id=user_id) if selected else []
    return {"records": serialize_records(rows, model, client.odoo_session.url),
            "population_count": count, "sample_count": len(rows)}
