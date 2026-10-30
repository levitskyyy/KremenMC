package net.minecraft.network.chat;

import java.util.Locale;

public final class TextColor {
    private final int value;

    private TextColor(int value) { this.value = value & 0xFFFFFF; }

    public static TextColor fromRgb(int rgb) { return new TextColor(rgb); }

    public static TextColor parseColor(String s) {
        if (s.startsWith("#")) return fromRgb(Integer.parseInt(s.substring(1), 16));
        Integer named = NAMED.get(s.toLowerCase(Locale.ROOT));
        if (named == null) throw new IllegalArgumentException("unknown color " + s);
        return fromRgb(named);
    }

    private static final java.util.Map<String, Integer> NAMED = java.util.Map.ofEntries(
            java.util.Map.entry("black", 0x000000), java.util.Map.entry("dark_blue", 0x0000AA),
            java.util.Map.entry("dark_green", 0x00AA00), java.util.Map.entry("dark_aqua", 0x00AAAA),
            java.util.Map.entry("dark_red", 0xAA0000), java.util.Map.entry("dark_purple", 0xAA00AA),
            java.util.Map.entry("gold", 0xFFAA00), java.util.Map.entry("gray", 0xAAAAAA),
            java.util.Map.entry("dark_gray", 0x555555), java.util.Map.entry("blue", 0x5555FF),
            java.util.Map.entry("green", 0x55FF55), java.util.Map.entry("aqua", 0x55FFFF),
            java.util.Map.entry("red", 0xFF5555), java.util.Map.entry("light_purple", 0xFF55FF),
            java.util.Map.entry("yellow", 0xFFFF55), java.util.Map.entry("white", 0xFFFFFF));

    public int getValue() { return value; }

    public String serialize() { return String.format(Locale.ROOT, "#%06X", value); }

    @Override public boolean equals(Object o) { return o instanceof TextColor t && t.value == value; }
    @Override public int hashCode() { return value; }
    @Override public String toString() { return serialize(); }
}
