--[[
  Kitchen-sink fixture: a single document that uses every construct the
  parser supports. TestParseTestdataKitchenSink checks each decoded value,
  so every field here is load-bearing; TestParseTableTestdataKitchenSink
  checks the exact key types and the [7] / ["7"] distinction.
]]

-- A line comment before the constructor.
{
    -- Numbers: integers, floats, exponents and hexadecimal forms.
    int = 42,
    int_negative = -7,
    int_max = 9223372036854775807,
    float = 1.5,
    float_leading_dot = .5,
    float_trailing_dot = 1.,
    float_exponent = 1e3,
    float_small_exponent = 2.5E-2,
    hex = 0xFF,
    hex_wrapped = 0xFFFFFFFFFFFFFFFF,
    hex_float = 0x1.8p1,

    -- Strings: both quote styles, escapes, unicode and long brackets.
    double_quoted = "double",
    single_quoted = 'single',
    escaped_tab = "a\tb",
    escaped_newline = "a\nb",
    escaped_carriage_return = "a\rb",
    escaped_controls = "\a\b\f\v",
    escaped_backslash = "\\",
    escaped_quotes = "\"\'",
    decimal_escapes = "\65\66\67",
    hex_escapes = "\x41\x42",
    unicode_escapes = "\u{48}\u{49}",
    skipped_whitespace = "a\z
        b",
    long_string = [[first
second]],
    leveled_long_string = [==[contains ]=] inside]==],

    -- Keys of every supported type.
    ["spaced key"] = "quoted",
    [7] = "integer key",
    ["7"] = "text seven",
    [-3] = "negative integer key",
    [1.5] = "float key",
    [true] = "boolean key",
    [false] = "other boolean key",
    end = "reserved word key",

    -- Scalars.
    nothing = nil,
    yes = true,
    no = false,

    -- Structures.
    list = { 1, 2.5, "three", true, nil },
    semicolons = { 1; 2; 3; },
    empty = {},
    nested = { one = { two = { three = "bottom" } } },
    mixed = { "first", 2, name = "mixed", [true] = "flag" },
    parenthesized = (1),
    unary_minus = - 2,
    trailing_separator = { 1, 2, },
    --[[ an inline block comment ]]
    last = "done",
}
-- A trailing comment after the constructor.
