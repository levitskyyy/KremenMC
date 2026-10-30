package net.minecraft.network.chat;

import java.util.Objects;

public final class Style {
    public static final Style EMPTY = new Style(null, null, null, null, null, null, null, null, null);

    private final TextColor color;
    private final Integer shadowColor;
    private final Boolean bold;
    private final Boolean italic;
    private final Boolean underlined;
    private final Boolean strikethrough;
    private final Boolean obfuscated;
    private final String insertion;
    private final FontDescription font;

    private Style(TextColor color, Integer shadowColor, Boolean bold, Boolean italic, Boolean underlined,
                  Boolean strikethrough, Boolean obfuscated, String insertion, FontDescription font) {
        this.color = color;
        this.shadowColor = shadowColor;
        this.bold = bold;
        this.italic = italic;
        this.underlined = underlined;
        this.strikethrough = strikethrough;
        this.obfuscated = obfuscated;
        this.insertion = insertion;
        this.font = font;
    }

    public TextColor getColor() { return color; }
    public Integer getShadowColor() { return shadowColor; }
    public boolean isBold() { return bold == Boolean.TRUE; }
    public boolean isItalic() { return italic == Boolean.TRUE; }
    public boolean isUnderlined() { return underlined == Boolean.TRUE; }
    public boolean isStrikethrough() { return strikethrough == Boolean.TRUE; }
    public boolean isObfuscated() { return obfuscated == Boolean.TRUE; }
    public Boolean bold() { return bold; }
    public Boolean italic() { return italic; }
    public Boolean underlined() { return underlined; }
    public Boolean strikethrough() { return strikethrough; }
    public Boolean obfuscated() { return obfuscated; }
    public String getInsertion() { return insertion; }
    public FontDescription getFont() { return font; }
    public boolean isEmpty() { return equals(EMPTY); }

    public Style withColor(TextColor c) { return new Style(c, shadowColor, bold, italic, underlined, strikethrough, obfuscated, insertion, font); }
    public Style withColor(int rgb) { return withColor(TextColor.fromRgb(rgb)); }
    public Style withShadowColor(int argb) { return new Style(color, argb, bold, italic, underlined, strikethrough, obfuscated, insertion, font); }
    public Style withoutShadow() { return new Style(color, null, bold, italic, underlined, strikethrough, obfuscated, insertion, font); }
    public Style withBold(Boolean v) { return new Style(color, shadowColor, v, italic, underlined, strikethrough, obfuscated, insertion, font); }
    public Style withItalic(Boolean v) { return new Style(color, shadowColor, bold, v, underlined, strikethrough, obfuscated, insertion, font); }
    public Style withUnderlined(Boolean v) { return new Style(color, shadowColor, bold, italic, v, strikethrough, obfuscated, insertion, font); }
    public Style withStrikethrough(Boolean v) { return new Style(color, shadowColor, bold, italic, underlined, v, obfuscated, insertion, font); }
    public Style withObfuscated(Boolean v) { return new Style(color, shadowColor, bold, italic, underlined, strikethrough, v, insertion, font); }
    public Style withInsertion(String v) { return new Style(color, shadowColor, bold, italic, underlined, strikethrough, obfuscated, v, font); }
    public Style withFont(FontDescription v) { return new Style(color, shadowColor, bold, italic, underlined, strikethrough, obfuscated, insertion, v); }

    /** Returns this style with unset properties taken from parent. */
    public Style applyTo(Style parent) {
        if (this == EMPTY) return parent;
        if (parent == EMPTY) return this;
        return new Style(color != null ? color : parent.color, shadowColor != null ? shadowColor : parent.shadowColor,
                bold != null ? bold : parent.bold, italic != null ? italic : parent.italic,
                underlined != null ? underlined : parent.underlined, strikethrough != null ? strikethrough : parent.strikethrough,
                obfuscated != null ? obfuscated : parent.obfuscated, insertion != null ? insertion : parent.insertion,
                font != null ? font : parent.font);
    }

    @Override
    public boolean equals(Object o) {
        return o instanceof Style s && Objects.equals(color, s.color) && Objects.equals(shadowColor, s.shadowColor)
                && Objects.equals(bold, s.bold) && Objects.equals(italic, s.italic) && Objects.equals(underlined, s.underlined)
                && Objects.equals(strikethrough, s.strikethrough) && Objects.equals(obfuscated, s.obfuscated)
                && Objects.equals(insertion, s.insertion) && Objects.equals(font, s.font);
    }

    @Override public int hashCode() { return Objects.hash(color, shadowColor, bold, italic, underlined, strikethrough, obfuscated, insertion, font); }

    @Override
    public String toString() {
        return "Style{color=" + color + ", bold=" + bold + ", italic=" + italic + ", underlined=" + underlined
                + ", strikethrough=" + strikethrough + ", obfuscated=" + obfuscated + ", font=" + font + "}";
    }
}
