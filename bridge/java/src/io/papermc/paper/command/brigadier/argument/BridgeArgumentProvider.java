package io.papermc.paper.command.brigadier.argument;

import com.destroystokyo.paper.profile.PlayerProfile;
import com.google.common.collect.Range;
import com.mojang.brigadier.StringReader;
import com.mojang.brigadier.arguments.ArgumentType;
import com.mojang.brigadier.exceptions.CommandSyntaxException;
import com.mojang.brigadier.exceptions.DynamicCommandExceptionType;
import com.mojang.brigadier.exceptions.SimpleCommandExceptionType;
import com.mojang.brigadier.LiteralMessage;
import io.papermc.paper.command.brigadier.CommandSourceStack;
import io.papermc.paper.command.brigadier.argument.predicate.BlockInWorldPredicate;
import io.papermc.paper.command.brigadier.argument.predicate.ItemStackPredicate;
import io.papermc.paper.command.brigadier.argument.range.DoubleRangeProvider;
import io.papermc.paper.command.brigadier.argument.range.IntegerRangeProvider;
import io.papermc.paper.command.brigadier.argument.resolvers.AngleResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.BlockPositionResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.ColumnBlockPositionResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.ColumnFinePositionResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.FinePositionResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.PlayerProfileListResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.RotationResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.selector.EntitySelectorArgumentResolver;
import io.papermc.paper.command.brigadier.argument.resolvers.selector.PlayerSelectorArgumentResolver;
import io.papermc.paper.entity.LookAnchor;
import io.papermc.paper.math.Position;
import io.papermc.paper.math.Rotation;
import io.papermc.paper.registry.RegistryAccess;
import io.papermc.paper.registry.RegistryKey;
import io.papermc.paper.registry.TypedKey;
import java.util.ArrayList;
import java.util.Collection;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.CompletableFuture;
import net.kyori.adventure.chat.SignedMessage;
import net.kyori.adventure.key.Key;
import net.kyori.adventure.text.Component;
import net.kyori.adventure.text.format.NamedTextColor;
import net.kyori.adventure.text.format.Style;
import net.kyori.adventure.text.format.TextColor;
import net.kyori.adventure.text.serializer.gson.GsonComponentSerializer;
import org.bukkit.Bukkit;
import org.bukkit.GameMode;
import org.bukkit.HeightMap;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.NamespacedKey;
import org.bukkit.World;
import org.bukkit.block.BlockState;
import org.bukkit.block.structure.Mirror;
import org.bukkit.block.structure.StructureRotation;
import org.bukkit.entity.Entity;
import org.bukkit.entity.Player;
import org.bukkit.inventory.ItemStack;
import org.bukkit.scoreboard.Criteria;
import org.bukkit.scoreboard.DisplaySlot;

/**
 * Paper's ArgumentTypes for the kremenmc bridge. Each type carries the
 * vanilla parser id it is advertised as; selectors resolve through
 * Server.selectEntities, which the Go server answers. Lives in Paper's
 * package because the provider interface is package-private.
 */
public final class BridgeArgumentProvider implements VanillaArgumentProvider {
    private static final SimpleCommandExceptionType TOO_MANY_ENTITIES = simple("argument.entity.toomany");
    private static final SimpleCommandExceptionType TOO_MANY_PLAYERS = simple("argument.player.toomany");
    private static final SimpleCommandExceptionType NO_ENTITY = simple("argument.entity.notfound.entity");
    private static final SimpleCommandExceptionType NO_PLAYER = simple("argument.entity.notfound.player");
    private static final SimpleCommandExceptionType MIXED = simple("argument.pos.mixed");
    private static final SimpleCommandExceptionType INCOMPLETE = simple("argument.pos3d.incomplete");
    private static final DynamicCommandExceptionType INVALID = new DynamicCommandExceptionType(o -> new LiteralMessage("Invalid value: " + o));

    private static SimpleCommandExceptionType simple(String key) {
        return new SimpleCommandExceptionType(io.papermc.paper.command.brigadier.MessageComponentSerializer.message().serialize(Component.translatable(key)));
    }

    @FunctionalInterface
    public interface Parser<T> {
        T parse(StringReader r) throws CommandSyntaxException;
    }

    /** An argument type known to the client by its vanilla parser id. */
    public record Native<T>(String parser, Map<String, Object> props, Parser<T> reader, List<String> examples) implements ArgumentType<T> {
        @Override public T parse(StringReader r) throws CommandSyntaxException { return reader.parse(r); }
        @Override public Collection<String> getExamples() { return examples; }
    }

    private static <T> Native<T> of(String parser, Parser<T> p, String... examples) {
        return new Native<>(parser, Map.of(), p, List.of(examples));
    }

    private static <T> Native<T> of(String parser, Map<String, Object> props, Parser<T> p, String... examples) {
        return new Native<>(parser, props, p, List.of(examples));
    }

    private static <T> Native<T> unsupported(String parser, String what) {
        return of(parser, r -> {
            throw INVALID.createWithContext(r, what + " arguments are not supported by the kremenmc bridge");
        });
    }

    // Readers

    static String token(StringReader r) {
        int start = r.getCursor();
        while (r.canRead() && r.peek() != ' ') r.skip();
        return r.getString().substring(start, r.getCursor());
    }

    /** A selector, player name or UUID, bracketed options included. */
    static String selector(StringReader r) throws CommandSyntaxException {
        int start = r.getCursor();
        if (r.canRead() && r.peek() == '@') {
            r.skip();
            if (r.canRead()) r.skip();
            if (r.canRead() && r.peek() == '[') {
                int depth = 0;
                boolean quoted = false;
                while (r.canRead()) {
                    char c = r.read();
                    if (c == '"') quoted = !quoted;
                    else if (!quoted && c == '[') depth++;
                    else if (!quoted && c == ']' && --depth == 0) break;
                }
            }
        } else {
            while (r.canRead() && r.peek() != ' ') r.skip();
        }
        String s = r.getString().substring(start, r.getCursor());
        if (s.isEmpty()) throw CommandSyntaxException.BUILT_IN_EXCEPTIONS.readerExpectedSymbol().createWithContext(r, "selector");
        return s;
    }

    static String resource(StringReader r) {
        int start = r.getCursor();
        while (r.canRead() && isResourceChar(r.peek())) r.skip();
        return r.getString().substring(start, r.getCursor());
    }

    private static boolean isResourceChar(char c) {
        return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '_' || c == ':' || c == '/' || c == '.' || c == '-';
    }

    private static NamespacedKey key(StringReader r) throws CommandSyntaxException {
        int start = r.getCursor();
        NamespacedKey k = NamespacedKey.fromString(resource(r));
        if (k == null) {
            r.setCursor(start);
            throw INVALID.createWithContext(r, "resource location");
        }
        return k;
    }

    private record Coord(boolean relative, boolean local, double value) {
        double resolve(double base) { return relative ? base + value : value; }
    }

    private static Coord coord(StringReader r, boolean integer, boolean center) throws CommandSyntaxException {
        if (!r.canRead()) throw INCOMPLETE.createWithContext(r);
        char c = r.peek();
        boolean rel = c == '~', loc = c == '^';
        if (rel || loc) {
            r.skip();
            double v = r.canRead() && r.peek() != ' ' ? r.readDouble() : 0;
            return new Coord(rel, loc, v);
        }
        int start = r.getCursor();
        double v = r.readDouble();
        String text = r.getString().substring(start, r.getCursor());
        if (integer && text.contains(".")) {
            r.setCursor(start);
            throw INVALID.createWithContext(r, text);
        }
        if (center && !text.contains(".")) v += 0.5;
        return new Coord(false, false, v);
    }

    private static Coord[] coords(StringReader r, int n, boolean integer, boolean center) throws CommandSyntaxException {
        Coord[] out = new Coord[n];
        for (int i = 0; i < n; i++) {
            if (i > 0) {
                if (!r.canRead() || r.peek() != ' ') throw INCOMPLETE.createWithContext(r);
                r.skip();
            }
            out[i] = coord(r, integer, center && i != 1);
        }
        boolean local = out[0].local;
        for (Coord c : out) if (c.local != local) throw MIXED.createWithContext(r);
        return out;
    }

    private static double[] position(Coord[] c, Location base) {
        if (c[0].local) {
            double yaw = Math.toRadians(base.getYaw() + 90), pitch = Math.toRadians(-base.getPitch());
            double yaw2 = Math.toRadians(base.getYaw() + 90), pitch2 = Math.toRadians(-base.getPitch() + 90);
            double[] fwd = {Math.cos(yaw) * Math.cos(pitch), Math.sin(pitch), Math.sin(yaw) * Math.cos(pitch)};
            double[] up = {Math.cos(yaw2) * Math.cos(pitch2), Math.sin(pitch2), Math.sin(yaw2) * Math.cos(pitch2)};
            double[] left = {fwd[1] * up[2] - fwd[2] * up[1], fwd[2] * up[0] - fwd[0] * up[2], fwd[0] * up[1] - fwd[1] * up[0]};
            double l = c[0].value, u = c.length > 2 ? c[1].value : 0, f = c[c.length - 1].value;
            return new double[] {base.getX() + left[0] * -l + up[0] * u + fwd[0] * f,
                    base.getY() + left[1] * -l + up[1] * u + fwd[1] * f,
                    base.getZ() + left[2] * -l + up[2] * u + fwd[2] * f};
        }
        if (c.length == 2) return new double[] {c[0].resolve(base.getX()), base.getY(), c[1].resolve(base.getZ())};
        return new double[] {c[0].resolve(base.getX()), c[1].resolve(base.getY()), c[2].resolve(base.getZ())};
    }

    private static List<Entity> select(CommandSourceStack src, String sel) {
        return Bukkit.selectEntities(src.getSender(), sel);
    }

    private static Map<String, Object> entityProps(boolean single, boolean players) {
        Map<String, Object> m = new LinkedHashMap<>();
        if (single) m.put("single", true);
        if (players) m.put("players", true);
        return m;
    }

    private static List<Player> players(List<Entity> es) {
        List<Player> out = new ArrayList<>();
        for (Entity e : es) if (e instanceof Player p) out.add(p);
        return out;
    }

    // VanillaArgumentProvider

    @Override
    public ArgumentType<EntitySelectorArgumentResolver> entity() {
        return of("minecraft:entity", entityProps(true, false), r -> {
            String s = selector(r);
            return src -> {
                List<Entity> es = select(src, s);
                if (es.isEmpty()) throw NO_ENTITY.create();
                if (es.size() > 1) throw TOO_MANY_ENTITIES.create();
                return es;
            };
        }, "Player", "0123", "@e", "@e[type=foo]", "dd12be42-52a9-4a91-a8a1-11c01849e498");
    }

    @Override
    public ArgumentType<PlayerSelectorArgumentResolver> player() {
        return of("minecraft:entity", entityProps(true, true), r -> {
            String s = selector(r);
            return src -> {
                List<Player> ps = players(select(src, s));
                if (ps.isEmpty()) throw NO_PLAYER.create();
                if (ps.size() > 1) throw TOO_MANY_PLAYERS.create();
                return ps;
            };
        }, "Player", "0123", "@p", "dd12be42-52a9-4a91-a8a1-11c01849e498");
    }

    @Override
    public ArgumentType<EntitySelectorArgumentResolver> entities() {
        return of("minecraft:entity", entityProps(false, false), r -> {
            String s = selector(r);
            return src -> {
                List<Entity> es = select(src, s);
                if (es.isEmpty()) throw NO_ENTITY.create();
                return es;
            };
        }, "Player", "0123", "@e", "@e[type=foo]");
    }

    @Override
    public ArgumentType<PlayerSelectorArgumentResolver> players() {
        return of("minecraft:entity", entityProps(false, true), r -> {
            String s = selector(r);
            return src -> {
                List<Player> ps = players(select(src, s));
                if (ps.isEmpty()) throw NO_PLAYER.create();
                return ps;
            };
        }, "Player", "0123", "@a", "@p");
    }

    @Override
    public ArgumentType<PlayerProfileListResolver> playerProfiles() {
        return of("minecraft:game_profile", r -> {
            String s = selector(r);
            return src -> {
                List<PlayerProfile> out = new ArrayList<>();
                if (s.startsWith("@")) {
                    for (Player p : players(select(src, s))) out.add(p.getPlayerProfile());
                } else {
                    Player p = Bukkit.getPlayerExact(s);
                    out.add(p != null ? p.getPlayerProfile() : Bukkit.createProfile(s));
                }
                return out;
            };
        }, "Player", "0123", "@e");
    }

    @Override
    public ArgumentType<BlockPositionResolver> blockPosition() {
        return of("minecraft:block_pos", r -> {
            Coord[] c = coords(r, 3, true, false);
            return src -> {
                double[] p = position(c, src.getLocation());
                return Position.block((int) Math.floor(p[0]), (int) Math.floor(p[1]), (int) Math.floor(p[2]));
            };
        }, "0 0 0", "~ ~ ~", "^ ^ ^", "^1 ^ ^-5", "~0.5 ~1 ~-5");
    }

    @Override
    public ArgumentType<ColumnBlockPositionResolver> columnBlockPosition() {
        return unsupported("minecraft:column_pos", "column position");
    }

    @Override
    public ArgumentType<FinePositionResolver> finePosition(boolean centerIntegers) {
        return of("minecraft:vec3", r -> {
            Coord[] c = coords(r, 3, false, centerIntegers);
            return src -> {
                double[] p = position(c, src.getLocation());
                return Position.fine(p[0], p[1], p[2]);
            };
        }, "0 0 0", "~ ~ ~", "^ ^ ^", "^1 ^ ^-5", "0.1 -0.5 .9", "~0.5 ~1 ~-5");
    }

    @Override
    public ArgumentType<ColumnFinePositionResolver> columnFinePosition(boolean centerIntegers) {
        return unsupported("minecraft:vec2", "column position");
    }

    @Override
    public ArgumentType<RotationResolver> rotation() {
        return of("minecraft:rotation", r -> {
            Coord[] c = coords(r, 2, false, false);
            return src -> {
                Location l = src.getLocation();
                return Rotation.rotation((float) c[0].resolve(l.getYaw()), (float) c[1].resolve(l.getPitch()));
            };
        }, "0 0", "~ ~", "~-5 ~5");
    }

    @Override
    public ArgumentType<AngleResolver> angle() {
        return of("minecraft:angle", r -> {
            Coord c = coord(r, false, false);
            return src -> (float) c.resolve(src.getLocation().getYaw());
        }, "0", "~", "~-5");
    }

    @Override public ArgumentType<AxisSet> axes() { return unsupported("minecraft:swizzle", "axis"); }
    @Override public ArgumentType<BlockState> blockState() { return unsupported("minecraft:block_state", "block state"); }
    @Override public ArgumentType<BlockInWorldPredicate> blockInWorldPredicate() { return unsupported("minecraft:block_predicate", "block predicate"); }

    @Override
    public ArgumentType<ItemStack> itemStack() {
        return of("minecraft:item_stack", r -> {
            int start = r.getCursor();
            NamespacedKey k = key(r);
            if (r.canRead() && r.peek() == '[') {
                int depth = 0;
                while (r.canRead()) {
                    char c = r.read();
                    if (c == '[') depth++;
                    else if (c == ']' && --depth == 0) break;
                }
            }
            Material m = Material.matchMaterial(k.toString());
            if (m == null || !m.isItem()) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, k);
            }
            return ItemStack.of(m);
        }, "stick", "minecraft:stick", "stick{foo=bar}");
    }

    @Override public ArgumentType<ItemStackPredicate> itemStackPredicate() { return unsupported("minecraft:item_predicate", "item predicate"); }

    @Override
    public ArgumentType<NamedTextColor> namedColor() {
        return of("minecraft:team_color", r -> {
            int start = r.getCursor();
            String s = r.readUnquotedString();
            NamedTextColor c = NamedTextColor.NAMES.value(s.toLowerCase(Locale.ROOT));
            if (c == null) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, s);
            }
            return c;
        }, "red", "green");
    }

    @Override
    public ArgumentType<TextColor> hexColor() {
        return of("minecraft:hex_color", r -> {
            int start = r.getCursor();
            String s = r.readUnquotedString();
            TextColor c = TextColor.fromHexString(s.startsWith("#") ? s : "#" + s);
            if (c == null) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, s);
            }
            return c;
        }, "F00", "FF0000");
    }

    @Override
    public ArgumentType<Component> component() {
        return of("minecraft:component", r -> {
            String rest = r.getRemaining();
            try {
                Component c = GsonComponentSerializer.gson().deserialize(rest);
                r.setCursor(r.getTotalLength());
                return c;
            } catch (RuntimeException e) {
                throw INVALID.createWithContext(r, e.getMessage());
            }
        }, "\"hello world\"", "\"\"", "{\"text\":\"hello world\"}", "[\"\"]");
    }

    @Override public ArgumentType<Style> style() { return unsupported("minecraft:style", "style"); }

    @Override
    public ArgumentType<SignedMessageResolver> signedMessage() {
        return of("minecraft:message", r -> {
            String text = r.getRemaining();
            r.setCursor(r.getTotalLength());
            return new SignedMessageResolver() {
                @Override public String content() { return text; }

                @Override
                public CompletableFuture<SignedMessage> resolveSignedMessage(String name, com.mojang.brigadier.context.CommandContext<CommandSourceStack> ctx) {
                    return CompletableFuture.completedFuture(SignedMessage.system(text, Component.text(text)));
                }
            };
        }, "Hello world!", "foo", "@e", "Hello @p :)");
    }

    @Override
    public ArgumentType<DisplaySlot> scoreboardDisplaySlot() {
        return of("minecraft:scoreboard_slot", r -> {
            int start = r.getCursor();
            String s = token(r);
            for (DisplaySlot d : DisplaySlot.values()) if (d.getId().equals(s)) return d;
            r.setCursor(start);
            throw INVALID.createWithContext(r, s);
        }, "sidebar", "foo.bar");
    }

    @Override public ArgumentType<NamespacedKey> namespacedKey() { return of("minecraft:resource_location", BridgeArgumentProvider::key, "foo", "foo:bar", "012"); }
    @Override public ArgumentType<Key> key() { return of("minecraft:resource_location", BridgeArgumentProvider::key, "foo", "foo:bar", "012"); }

    @Override
    public ArgumentType<IntegerRangeProvider> integerRange() {
        return of("minecraft:int_range", r -> {
            Range<Integer> range = range(r, true, Integer::parseInt);
            return () -> range;
        }, "0..5", "0", "-5", "-100..", "..100");
    }

    @Override
    public ArgumentType<DoubleRangeProvider> doubleRange() {
        return of("minecraft:float_range", r -> {
            Range<Double> range = range(r, false, Double::parseDouble);
            return () -> range;
        }, "0..5.2", "0", "-5.4", "-100.76..", "..100");
    }

    private static <T extends Comparable<T>> Range<T> range(StringReader r, boolean integer, java.util.function.Function<String, T> conv) throws CommandSyntaxException {
        int start = r.getCursor();
        String s = token(r);
        try {
            int i = s.indexOf("..");
            if (i < 0) return Range.singleton(conv.apply(s));
            String lo = s.substring(0, i), hi = s.substring(i + 2);
            if (lo.isEmpty() && hi.isEmpty()) throw new IllegalArgumentException();
            if (lo.isEmpty()) return Range.atMost(conv.apply(hi));
            if (hi.isEmpty()) return Range.atLeast(conv.apply(lo));
            return Range.closed(conv.apply(lo), conv.apply(hi));
        } catch (IllegalArgumentException e) {
            r.setCursor(start);
            throw INVALID.createWithContext(r, s);
        }
    }

    @Override
    public ArgumentType<World> world() {
        return of("minecraft:dimension", r -> {
            int start = r.getCursor();
            NamespacedKey k = key(r);
            World w = Bukkit.getWorld(k);
            if (w == null) w = Bukkit.getWorld(k.getKey());
            if (w == null) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, k);
            }
            return w;
        }, "world", "minecraft:overworld");
    }

    @Override
    public ArgumentType<GameMode> gameMode() {
        return of("minecraft:gamemode", r -> {
            int start = r.getCursor();
            String s = r.readUnquotedString();
            try {
                return GameMode.valueOf(s.toUpperCase(Locale.ROOT));
            } catch (IllegalArgumentException e) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, s);
            }
        }, "survival", "creative");
    }

    @Override public ArgumentType<HeightMap> heightMap() { return unsupported("minecraft:heightmap", "height map"); }

    @Override
    public ArgumentType<UUID> uuid() {
        return of("minecraft:uuid", r -> {
            int start = r.getCursor();
            String s = token(r);
            try {
                return UUID.fromString(s);
            } catch (IllegalArgumentException e) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, s);
            }
        }, "dd12be42-52a9-4a91-a8a1-11c01849e498");
    }

    @Override public ArgumentType<Criteria> objectiveCriteria() { return unsupported("minecraft:objective_criteria", "objective criteria"); }

    @Override
    public ArgumentType<LookAnchor> entityAnchor() {
        return of("minecraft:entity_anchor", r -> {
            int start = r.getCursor();
            String s = r.readUnquotedString();
            for (LookAnchor a : LookAnchor.values()) if (a.name().equalsIgnoreCase(s)) return a;
            r.setCursor(start);
            throw INVALID.createWithContext(r, s);
        }, "eyes", "feet");
    }

    @Override
    public ArgumentType<Integer> time(int minTicks) {
        Map<String, Object> props = Map.of("min", (double) minTicks);
        return of("minecraft:time", props, r -> {
            int start = r.getCursor();
            float v = r.readFloat();
            int mul = 1;
            if (r.canRead()) {
                switch (r.peek()) {
                    case 'd' -> mul = 24000;
                    case 's' -> mul = 20;
                    case 't' -> mul = 1;
                    default -> mul = 0;
                }
                if (mul != 0) r.skip();
                else mul = 1;
            }
            int ticks = Math.round(v * mul);
            if (ticks < minTicks) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, ticks);
            }
            return ticks;
        }, "0d", "0s", "0t", "0");
    }

    @Override public ArgumentType<Mirror> templateMirror() { return unsupported("minecraft:template_mirror", "template mirror"); }
    @Override public ArgumentType<StructureRotation> templateRotation() { return unsupported("minecraft:template_rotation", "template rotation"); }

    @Override
    public <T> ArgumentType<TypedKey<T>> resourceKey(RegistryKey<T> registryKey) {
        return of("minecraft:resource_key", Map.of("registry", registryKey.key().asString()), r -> TypedKey.create(registryKey, key(r)), "foo", "foo:bar", "012");
    }

    @Override
    public <T> ArgumentType<T> resource(RegistryKey<T> registryKey) {
        return of("minecraft:resource", Map.of("registry", registryKey.key().asString()), r -> {
            int start = r.getCursor();
            NamespacedKey k = key(r);
            @SuppressWarnings({"unchecked", "rawtypes"})
            T v = (T) RegistryAccess.registryAccess().getRegistry((RegistryKey) registryKey).get(k);
            if (v == null) {
                r.setCursor(start);
                throw INVALID.createWithContext(r, k);
            }
            return v;
        }, "foo", "foo:bar", "012");
    }
}
