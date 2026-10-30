package net.minecraft.network.chat;

import java.util.ArrayList;
import java.util.List;
import java.util.Objects;

public class MutableComponent implements Component {
    private final ComponentContents contents;
    private final List<Component> siblings;
    private Style style;

    MutableComponent(ComponentContents contents, List<Component> siblings, Style style) {
        this.contents = contents;
        this.siblings = siblings;
        this.style = style;
    }

    public static MutableComponent create(ComponentContents contents) {
        return new MutableComponent(contents, new ArrayList<>(), Style.EMPTY);
    }

    @Override public ComponentContents getContents() { return contents; }
    @Override public Style getStyle() { return style; }
    @Override public List<Component> getSiblings() { return siblings; }

    public MutableComponent setStyle(Style style) {
        this.style = style == null ? Style.EMPTY : style;
        return this;
    }

    public MutableComponent append(String text) { return append(Component.literal(text)); }

    public MutableComponent append(Component c) {
        siblings.add(c);
        return this;
    }

    public MutableComponent withStyle(Style s) { return setStyle(s.applyTo(style)); }

    public MutableComponent withColor(int rgb) { return setStyle(style.withColor(TextColor.fromRgb(rgb))); }

    @Override
    public boolean equals(Object o) {
        return o instanceof MutableComponent m && contents.equals(m.contents) && style.equals(m.style) && siblings.equals(m.siblings);
    }

    @Override public int hashCode() { return Objects.hash(contents, style, siblings); }

    @Override public String toString() { return "Component{" + contents + ", " + style + ", " + siblings + "}"; }
}
