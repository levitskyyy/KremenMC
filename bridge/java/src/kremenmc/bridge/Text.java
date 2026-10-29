package kremenmc.bridge;

import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.serializer.gson.GsonComponentSerializer;
import net.kyori.adventure.text.serializer.legacy.LegacyComponentSerializer;
import net.kyori.adventure.text.serializer.plain.PlainTextComponentSerializer;
import net.md_5.bungee.api.chat.BaseComponent;
import net.md_5.bungee.chat.ComponentSerializer;

/** Conversions between Bukkit text forms and the JSON chat components sent to Go. */
final class Text {
    private Text() {}

    static String json(Component c) {
        return c == null ? null : GsonComponentSerializer.gson().serialize(c);
    }

    static String jsonLegacy(String legacy) {
        return legacy == null ? null : json(LegacyComponentSerializer.legacySection().deserialize(legacy));
    }

    static String json(BaseComponent... cs) {
        return ComponentSerializer.toString(cs);
    }

    static Component component(String json) {
        if (json == null || json.isEmpty()) return null;
        try {
            return GsonComponentSerializer.gson().deserialize(json);
        } catch (RuntimeException e) {
            return Component.text(json);
        }
    }

    static String legacy(Component c) {
        return c == null ? null : LegacyComponentSerializer.legacySection().serialize(c);
    }

    static String plain(Component c) {
        return c == null ? null : PlainTextComponentSerializer.plainText().serialize(c);
    }
}
