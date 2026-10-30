package net.minecraft.network.chat;

import java.util.List;
import net.minecraft.network.chat.contents.KeybindContents;
import net.minecraft.network.chat.contents.ObjectContents;
import net.minecraft.network.chat.contents.PlainTextContents;
import net.minecraft.network.chat.contents.TranslatableContents;
import net.minecraft.network.chat.contents.objects.ObjectInfo;

public interface Component {
    ComponentContents getContents();

    Style getStyle();

    List<Component> getSiblings();

    default String getString() {
        StringBuilder sb = new StringBuilder();
        appendText(this, sb);
        return sb.toString();
    }

    private static void appendText(Component c, StringBuilder sb) {
        switch (c.getContents()) {
            case PlainTextContents t -> sb.append(t.text());
            case TranslatableContents t -> sb.append(t.getFallback() != null ? t.getFallback() : t.getKey());
            case KeybindContents k -> sb.append(k.getName());
            default -> {}
        }
        for (Component s : c.getSiblings()) appendText(s, sb);
    }

    default MutableComponent plainCopy() { return MutableComponent.create(getContents()); }

    default MutableComponent copy() {
        MutableComponent m = plainCopy().setStyle(getStyle());
        for (Component s : getSiblings()) m.append(s);
        return m;
    }

    static MutableComponent empty() { return literal(""); }

    static MutableComponent literal(String text) { return MutableComponent.create(new PlainTextContents(text)); }

    static Component nullToEmpty(String text) { return text == null ? empty() : literal(text); }

    static MutableComponent translatable(String key) { return MutableComponent.create(new TranslatableContents(key, null, TranslatableContents.NO_ARGS)); }

    static MutableComponent translatable(String key, Object... args) { return MutableComponent.create(new TranslatableContents(key, null, args)); }

    static MutableComponent translatableWithFallback(String key, String fallback) { return MutableComponent.create(new TranslatableContents(key, fallback, TranslatableContents.NO_ARGS)); }

    static MutableComponent translatableWithFallback(String key, String fallback, Object... args) { return MutableComponent.create(new TranslatableContents(key, fallback, args)); }

    static MutableComponent keybind(String name) { return MutableComponent.create(new KeybindContents(name)); }

    static MutableComponent object(ObjectInfo info) { return MutableComponent.create(new ObjectContents(info)); }
}
