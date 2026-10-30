package kremenmc.bridge;

import com.google.gson.Gson;
import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.mojang.authlib.GameProfile;
import com.mojang.authlib.properties.Property;
import io.netty.buffer.ByteBuf;
import io.netty.channel.Channel;
import io.netty.channel.ChannelHandlerContext;
import io.netty.channel.ChannelOutboundHandlerAdapter;
import io.netty.channel.ChannelPromise;
import io.netty.channel.embedded.EmbeddedChannel;
import io.netty.util.ReferenceCountUtil;
import io.netty.util.internal.logging.InternalLoggerFactory;
import io.netty.util.internal.logging.JdkLoggerFactory;
import java.util.ArrayList;
import java.util.EnumSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import java.util.logging.Level;
import net.minecraft.network.Connection;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.FontDescription;
import net.minecraft.network.chat.Style;
import net.minecraft.network.chat.contents.KeybindContents;
import net.minecraft.network.chat.contents.ObjectContents;
import net.minecraft.network.chat.contents.PlainTextContents;
import net.minecraft.network.chat.contents.TranslatableContents;
import net.minecraft.network.chat.contents.objects.AtlasSprite;
import net.minecraft.network.chat.contents.objects.PlayerSprite;
import net.minecraft.network.chat.numbers.BlankFormat;
import net.minecraft.network.chat.numbers.FixedFormat;
import net.minecraft.network.chat.numbers.NumberFormat;
import net.minecraft.network.chat.numbers.StyledFormat;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.game.ClientboundPlayerInfoRemovePacket;
import net.minecraft.network.protocol.game.ClientboundPlayerInfoUpdatePacket;
import net.minecraft.network.protocol.game.ClientboundResetScorePacket;
import net.minecraft.network.protocol.game.ClientboundSetDisplayObjectivePacket;
import net.minecraft.network.protocol.game.ClientboundSetObjectivePacket;
import net.minecraft.network.protocol.game.ClientboundSetPlayerTeamPacket;
import net.minecraft.network.protocol.game.ClientboundSetScorePacket;
import net.minecraft.network.protocol.game.ClientboundTabListPacket;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.world.level.GameType;
import org.bukkit.craftbukkit.entity.CraftPlayer;
import org.bukkit.entity.Player;

/**
 * The NMS compatibility layer: each player gets a Netty channel (see Pipes)
 * whose "encoder" turns the shim packet objects into OP_SEND_PACKET
 * messages, and Go sends the real packets. Plugin handlers added before
 * "packet_handler" see every packet object, as on a vanilla pipeline;
 * packet bodies routed through the channel pass the encoder unchanged.
 */
final class Nms {
    private Nms() {}

    private static final Gson GSON = new Gson();
    private static final Set<String> unsupported = ConcurrentHashMap.newKeySet();
    private static final EnumSet<ClientboundPlayerInfoUpdatePacket.Action> ECHO_ACTIONS = EnumSet.of(
            ClientboundPlayerInfoUpdatePacket.Action.ADD_PLAYER, ClientboundPlayerInfoUpdatePacket.Action.UPDATE_GAME_MODE,
            ClientboundPlayerInfoUpdatePacket.Action.UPDATE_LISTED, ClientboundPlayerInfoUpdatePacket.Action.UPDATE_LATENCY);

    /** Loads the Netty and packet classes off the event path. */
    static void warmUp() {
        InternalLoggerFactory.setDefaultFactory(JdkLoggerFactory.INSTANCE);
        Stubs.prepare();
        EmbeddedChannel ch = new EmbeddedChannel(new Out(null, null));
        ch.writeAndFlush(new ClientboundPlayerInfoRemovePacket(List.of()));
        ch.close();
        encode(new ClientboundTabListPacket(Component.empty(), Component.empty()), new LinkedHashMap<>());
    }

    static ServerPlayer newHandle(Bridge b, Players.PlayerImpl p) {
        Pipes.Entry e = b.pipes.entry(p.id, Wire.str(p.snapshot, "address"));
        p.out = e.out();
        return new ServerPlayer((CraftPlayer) b.players.cached(p.id), p.getProfile(), e.connection());
    }

    static void close(Bridge b, Players.PlayerImpl p) {
        b.pipes.close(p.id);
    }

    /**
     * Go sends the player list itself; replaying those entries through the
     * pipeline lets plugin handlers track what the client's tab list holds.
     */
    static void echoAdd(Players.PlayerImpl to, List<Player> players) {
        if (to.handle == null || players.isEmpty()) return;
        List<ClientboundPlayerInfoUpdatePacket.Entry> entries = new ArrayList<>();
        for (Player p : players) {
            if (!(Stubs.implOf(p) instanceof Players.PlayerImpl pi)) continue;
            GameType gm = GameType.byName(Wire.str(pi.snapshot, "gameMode").toLowerCase(java.util.Locale.ROOT));
            entries.add(new ClientboundPlayerInfoUpdatePacket.Entry(pi.id, pi.getProfile(), true, 0, gm, null, true, 0, null));
        }
        echo(to, new ClientboundPlayerInfoUpdatePacket(ECHO_ACTIONS, entries));
    }

    static void echoRemove(Players.PlayerImpl to, UUID gone) {
        if (to.handle != null) echo(to, new ClientboundPlayerInfoRemovePacket(List.of(gone)));
    }

    private static void echo(Players.PlayerImpl to, Packet<?> pkt) {
        Channel ch = to.handle.connection.connection.channel;
        if (!ch.isOpen()) return;
        Out out = to.out;
        ch.eventLoop().execute(() -> {
            out.echo = pkt;
            try {
                ch.writeAndFlush(pkt);
            } finally {
                out.echo = null;
            }
        });
    }

    static final class Out extends ChannelOutboundHandlerAdapter {
        private final Bridge b;
        private final UUID id;
        Object echo;

        Out(Bridge b, UUID id) {
            this.b = b;
            this.id = id;
        }

        @Override
        public void write(ChannelHandlerContext ctx, Object msg, ChannelPromise promise) {
            if (msg instanceof ByteBuf || msg instanceof PlayerChannel.Marker) {
                ctx.write(msg, promise);
                return;
            }
            try {
                if (echo == null) {
                    forward(msg);
                } else if (msg != echo && msg instanceof ClientboundPlayerInfoUpdatePacket p) {
                    // A handler rewrote a server-sent entry; the client already has it, so only the updates matter.
                    EnumSet<ClientboundPlayerInfoUpdatePacket.Action> a = EnumSet.copyOf(p.actions());
                    a.remove(ClientboundPlayerInfoUpdatePacket.Action.ADD_PLAYER);
                    a.remove(ClientboundPlayerInfoUpdatePacket.Action.INITIALIZE_CHAT);
                    if (!a.isEmpty()) forward(new ClientboundPlayerInfoUpdatePacket(a, p.entries()));
                }
                promise.trySuccess();
            } catch (RuntimeException e) {
                promise.tryFailure(e);
                if (b != null) b.log.log(Level.WARNING, "kremenmc bridge: cannot send " + msg.getClass().getSimpleName(), e);
            } finally {
                ReferenceCountUtil.release(msg);
            }
        }

        private void forward(Object msg) {
            Map<String, Object> f = new LinkedHashMap<>();
            String kind = encode(msg, f);
            if (b == null) return;
            if (kind != null) {
                b.conn.notify(Wire.OP_SEND_PACKET, id, kind, f);
            } else if (unsupported.add(msg.getClass().getName())) {
                b.log.info("kremenmc bridge: packet " + msg.getClass().getName() + " is not supported and was dropped");
            }
        }
    }

    /** Fills f with the packet's fields and returns its kind, or null if the bridge cannot send it. */
    static String encode(Object msg, Map<String, Object> f) {
        switch (msg) {
            case ClientboundTabListPacket p -> {
                f.put("header", json(p.header()));
                f.put("footer", json(p.footer()));
                return "tab_list";
            }
            case ClientboundPlayerInfoUpdatePacket p -> {
                int mask = 0;
                for (ClientboundPlayerInfoUpdatePacket.Action a : p.actions()) mask |= 1 << a.ordinal();
                List<Object> entries = new ArrayList<>();
                for (ClientboundPlayerInfoUpdatePacket.Entry e : p.entries()) entries.add(entry(e));
                f.put("actions", mask);
                f.put("entries", entries);
                return "player_info_update";
            }
            case ClientboundPlayerInfoRemovePacket p -> {
                f.put("uuids", new ArrayList<Object>(p.profileIds()));
                return "player_info_remove";
            }
            case ClientboundSetObjectivePacket p -> {
                f.put("name", p.getObjectiveName());
                f.put("method", p.getMethod());
                f.put("displayName", json(p.getDisplayName()));
                f.put("renderType", p.getRenderType() == null ? 0 : p.getRenderType().ordinal());
                f.put("numberFormat", numberFormat(p.getNumberFormat()));
                return "set_objective";
            }
            case ClientboundSetDisplayObjectivePacket p -> {
                f.put("slot", p.getSlot().id());
                f.put("name", p.getObjectiveName());
                return "set_display_objective";
            }
            case ClientboundSetScorePacket p -> {
                f.put("owner", p.owner());
                f.put("objective", p.objectiveName());
                f.put("score", p.score());
                f.put("displayName", p.display().map(Nms::json).orElse(null));
                f.put("numberFormat", numberFormat(p.numberFormat()));
                return "set_score";
            }
            case ClientboundResetScorePacket p -> {
                f.put("owner", p.owner());
                f.put("objective", p.objectiveName());
                return "reset_score";
            }
            case ClientboundSetPlayerTeamPacket p -> {
                f.put("name", p.getName());
                f.put("method", p.getMethodId());
                f.put("players", new ArrayList<Object>(p.getPlayers()));
                p.getParameters().ifPresent(t -> {
                    Map<String, Object> m = new LinkedHashMap<>();
                    m.put("displayName", json(t.getDisplayName()));
                    m.put("prefix", json(t.getPlayerPrefix()));
                    m.put("suffix", json(t.getPlayerSuffix()));
                    m.put("visibility", t.getNametagVisibility().id);
                    m.put("collision", t.getCollisionRule().id);
                    m.put("color", t.getColor().map(c -> c.getId()).orElse(-1));
                    m.put("options", t.getOptions());
                    f.put("params", m);
                });
                return "set_player_team";
            }
            default -> {
                return null;
            }
        }
    }

    private static Map<String, Object> entry(ClientboundPlayerInfoUpdatePacket.Entry e) {
        Map<String, Object> m = new LinkedHashMap<>();
        m.put("uuid", e.profileId());
        GameProfile gp = e.profile();
        if (gp != null) {
            m.put("name", gp.name() == null ? "" : gp.name());
            List<Object> props = new ArrayList<>();
            for (Property pr : gp.properties().values()) {
                Map<String, Object> pm = new LinkedHashMap<>();
                pm.put("name", pr.name());
                pm.put("value", pr.value());
                pm.put("signature", pr.signature());
                props.add(pm);
            }
            m.put("properties", props);
        }
        m.put("listed", e.listed());
        m.put("latency", e.latency());
        m.put("gameMode", e.gameMode() == null ? 0 : e.gameMode().getId());
        m.put("displayName", json(e.displayName()));
        m.put("listOrder", e.listOrder());
        m.put("showHat", e.showHat());
        return m;
    }

    private static Map<String, Object> numberFormat(Optional<NumberFormat> nf) {
        if (nf == null || nf.isEmpty()) return null;
        Map<String, Object> m = new LinkedHashMap<>();
        switch (nf.get()) {
            case BlankFormat x -> m.put("type", 0);
            case StyledFormat s -> {
                m.put("type", 1);
                m.put("value", json(Component.empty().setStyle(s.style())));
            }
            case FixedFormat x -> {
                m.put("type", 2);
                m.put("value", json(x.value()));
            }
            default -> {
                return null;
            }
        }
        return m;
    }

    static String json(Component c) {
        return c == null ? null : GSON.toJson(tree(c, 0));
    }

    private static JsonElement tree(Component c, int depth) {
        JsonObject o = new JsonObject();
        if (depth > 64) {
            o.addProperty("text", "");
            return o;
        }
        switch (c.getContents()) {
            case PlainTextContents t -> o.addProperty("text", t.text());
            case TranslatableContents t -> {
                o.addProperty("translate", t.getKey());
                if (t.getFallback() != null) o.addProperty("fallback", t.getFallback());
                if (t.getArgs().length > 0) {
                    JsonArray with = new JsonArray();
                    for (Object a : t.getArgs()) {
                        if (a instanceof Component ac) {
                            with.add(tree(ac, depth + 1));
                        } else {
                            JsonObject s = new JsonObject();
                            s.addProperty("text", String.valueOf(a));
                            with.add(s);
                        }
                    }
                    o.add("with", with);
                }
            }
            case KeybindContents k -> o.addProperty("keybind", k.name());
            case ObjectContents oc -> {
                o.addProperty("type", "object");
                switch (oc.contents()) {
                    case AtlasSprite a -> {
                        o.addProperty("atlas", a.atlas().toString());
                        o.addProperty("sprite", a.sprite().toString());
                    }
                    case PlayerSprite ps -> {
                        JsonObject pl = new JsonObject();
                        ps.player().name().ifPresent(n -> pl.addProperty("name", n));
                        ps.player().id().ifPresent(u -> pl.addProperty("id", u.toString()));
                        o.add("player", pl);
                        o.addProperty("hat", ps.hat());
                    }
                    default -> {}
                }
            }
            default -> o.addProperty("text", "");
        }
        style(o, c.getStyle());
        if (!c.getSiblings().isEmpty()) {
            JsonArray extra = new JsonArray();
            for (Component s : c.getSiblings()) extra.add(tree(s, depth + 1));
            o.add("extra", extra);
        }
        return o;
    }

    private static void style(JsonObject o, Style s) {
        if (s == null || s.isEmpty()) return;
        if (s.getColor() != null) o.addProperty("color", s.getColor().serialize());
        if (s.getShadowColor() != null) o.addProperty("shadow_color", s.getShadowColor());
        if (s.bold() != null) o.addProperty("bold", s.bold());
        if (s.italic() != null) o.addProperty("italic", s.italic());
        if (s.underlined() != null) o.addProperty("underlined", s.underlined());
        if (s.strikethrough() != null) o.addProperty("strikethrough", s.strikethrough());
        if (s.obfuscated() != null) o.addProperty("obfuscated", s.obfuscated());
        if (s.getInsertion() != null) o.addProperty("insertion", s.getInsertion());
        if (s.getFont() instanceof FontDescription.Resource r) o.addProperty("font", r.id().toString());
    }
}
