-- Example configuration table exercising most supported syntax.
{
    name = "luatable",
    version = "1.0.0",
    debug = false,
    timeout = 30,
    ratio = 0.75,
    ["max-connections"] = 128,
    [42] = "answer",
    [true] = "yes",
    servers = {
        { host = "alpha", port = 8080 },
        { host = "beta", port = 8081 },
    },
    features = { "parse", "nested", "comments"; },
    nested = { level1 = { level2 = { level3 = "deep" } } },
    long = [[
multi-line
long string
    ]],
}
