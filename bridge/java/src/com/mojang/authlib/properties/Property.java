package com.mojang.authlib.properties;

public record Property(String name, String value, String signature) {
    public Property(String name, String value) { this(name, value, null); }

    public String getName() { return name; }
    public String getValue() { return value; }
    public String getSignature() { return signature; }
    public boolean hasSignature() { return signature != null; }
}
