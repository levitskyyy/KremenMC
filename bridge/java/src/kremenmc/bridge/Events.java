package kremenmc.bridge;

import io.papermc.paper.chat.ChatRenderer;
import io.papermc.paper.event.player.AsyncChatEvent;
import java.net.InetAddress;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.UUID;
import net.kyori.adventure.audience.Audience;
import net.kyori.adventure.chat.SignedMessage;
import net.kyori.adventure.text.Component;
import org.bukkit.Location;
import org.bukkit.World;
import org.bukkit.block.Block;
import org.bukkit.block.BlockFace;
import org.bukkit.entity.Player;
import org.bukkit.event.Event;
import org.bukkit.event.block.Action;
import org.bukkit.event.block.BlockBreakEvent;
import org.bukkit.event.block.BlockPlaceEvent;
import org.bukkit.event.player.AsyncPlayerChatEvent;
import org.bukkit.event.player.AsyncPlayerPreLoginEvent;
import org.bukkit.event.player.PlayerCommandPreprocessEvent;
import org.bukkit.event.player.PlayerInteractEvent;
import org.bukkit.event.player.PlayerJoinEvent;
import org.bukkit.event.player.PlayerMoveEvent;
import org.bukkit.event.player.PlayerQuitEvent;
import org.bukkit.inventory.EquipmentSlot;

/** Builds Bukkit events from Go event fields and reports cancellation and changed fields back. */
final class Events {
    private Events() {}

    private static final long SYNC_TIMEOUT_MS = 30_000;
    private static final String LEGACY_FORMAT = "<%1$s> %2$s";

    private static final Map<String, List<Class<? extends Event>>> WIRE = new LinkedHashMap<>();

    static {
        WIRE.put("AsyncPlayerPreLogin", List.of(AsyncPlayerPreLoginEvent.class));
        WIRE.put("PlayerJoin", List.of(PlayerJoinEvent.class));
        WIRE.put("PlayerQuit", List.of(PlayerQuitEvent.class));
        WIRE.put("Chat", List.of(AsyncChatEvent.class, AsyncPlayerChatEvent.class));
        WIRE.put("PlayerCommandPreprocess", List.of(PlayerCommandPreprocessEvent.class));
        WIRE.put("PlayerMove", List.of(PlayerMoveEvent.class));
        WIRE.put("BlockBreak", List.of(BlockBreakEvent.class));
        WIRE.put("BlockPlace", List.of(BlockPlaceEvent.class));
        WIRE.put("PlayerInteract", List.of(PlayerInteractEvent.class));
        WorldEvents.register(WIRE);
    }

    private static boolean listened(Class<? extends Event> c) {
        return PluginManagerImpl.handlerList(c).getRegisteredListeners().length > 0;
    }

    /** Join and quit are always forwarded so the player registry stays current. */
    static List<String> listenedNames() {
        List<String> out = new ArrayList<>(List.of("PlayerJoin", "PlayerQuit"));
        if (listened(org.bukkit.event.inventory.InventoryClickEvent.class)) out.add("InventoryClick");
        if (listened(org.bukkit.event.inventory.InventoryDragEvent.class)) out.add("InventoryDrag");
        for (Map.Entry<String, List<Class<? extends Event>>> e : WIRE.entrySet()) {
            if (out.contains(e.getKey())) continue;
            for (Class<? extends Event> c : e.getValue()) {
                if (listened(c)) {
                    out.add(e.getKey());
                    break;
                }
            }
        }
        return out;
    }

    static Object[] fire(Bridge b, String name, Map<String, Object> f, boolean async) throws Exception {
        if (async) return dispatch(b, name, f);
        return b.main.call(() -> dispatch(b, name, f), SYNC_TIMEOUT_MS);
    }

    private static Object[] dispatch(Bridge b, String name, Map<String, Object> f) throws Exception {
        Map<String, Object> out = new HashMap<>();
        boolean cancelled = switch (name) {
            case "AsyncPlayerPreLogin" -> preLogin(b, f, out);
            case "PlayerJoin" -> join(b, f, out);
            case "PlayerQuit" -> quit(b, f, out);
            case "Chat" -> chat(b, f, out);
            case "PlayerCommandPreprocess" -> command(b, f, out);
            case "PlayerMove" -> move(b, f, out);
            case "BlockBreak" -> blockBreak(b, f, out);
            case "BlockPlace" -> blockPlace(b, f, out);
            case "PlayerInteract" -> interact(b, f, out);
            default -> {
                Boolean r = WorldEvents.dispatch(b, name, f, out);
                if (r == null) throw new IllegalArgumentException("unknown event " + name);
                yield r;
            }
        };
        return new Object[] {cancelled, out};
    }

    private static Player player(Bridge b, Map<String, Object> f) {
        UUID id = (UUID) f.get("player");
        Player p = b.players.cached(id);
        if (p == null) p = b.players.get(id);
        if (p == null) throw new IllegalStateException("unknown player " + id);
        return p;
    }

    private static void call(Bridge b, Event e) {
        b.pluginManager.callEvent(e);
    }

    private static boolean preLogin(Bridge b, Map<String, Object> f, Map<String, Object> out) throws Exception {
        String name = Wire.str(f, "name");
        UUID id = (UUID) f.get("uuid");
        InetAddress addr;
        try {
            addr = InetAddress.ofLiteral(Wire.str(f, "address"));
        } catch (IllegalArgumentException e) {
            addr = InetAddress.getLoopbackAddress();
        }
        AsyncPlayerPreLoginEvent e = new AsyncPlayerPreLoginEvent(name, addr, addr, id, false, Players.profile(id, name));
        call(b, e);
        if (e.getLoginResult() == AsyncPlayerPreLoginEvent.Result.ALLOWED) return false;
        out.put("result", e.getLoginResult().name());
        out.put("kickMessage", Text.json(e.kickMessage()));
        return true;
    }

    private static boolean join(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Player p = b.players.track((UUID) f.get("player"), Wire.str(f, "name"));
        Component msg = Text.component(Wire.str(f, "joinMessage"));
        PlayerJoinEvent e = new PlayerJoinEvent(p, msg);
        call(b, e);
        echoJoin(b, p);
        Scoreboards.of(b).onJoin(p.getUniqueId());
        if (!Objects.equals(msg, e.joinMessage())) out.put("joinMessage", Text.json(e.joinMessage()));
        return false;
    }

    private static boolean quit(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        UUID id = (UUID) f.get("player");
        Player p = b.players.track(id, Wire.str(f, "name"));
        Component msg = Text.component(Wire.str(f, "quitMessage"));
        PlayerQuitEvent.QuitReason reason;
        try {
            reason = PlayerQuitEvent.QuitReason.valueOf(Wire.str(f, "reason"));
        } catch (IllegalArgumentException ex) {
            reason = PlayerQuitEvent.QuitReason.DISCONNECTED;
        }
        PlayerQuitEvent e = new PlayerQuitEvent(p, msg, reason);
        try {
            call(b, e);
        } finally {
            b.players.remove(id);
            Scoreboards.of(b).onQuit(id);
            BossBars.quit(id);
            echoQuit(b, p);
        }
        if (!Objects.equals(msg, e.quitMessage())) out.put("quitMessage", Text.json(e.quitMessage()));
        return false;
    }

    private static void echoJoin(Bridge b, Player joined) {
        List<Player> online = b.players.online();
        if (!online.contains(joined)) online.add(joined);
        for (Player o : online) {
            if (!(Stubs.implOf(o) instanceof Players.PlayerImpl pi)) continue;
            Nms.echoAdd(pi, o.equals(joined) ? online : List.of(joined));
        }
    }

    private static void echoQuit(Bridge b, Player gone) {
        if (Stubs.implOf(gone) instanceof Players.PlayerImpl g) Nms.close(b, g);
        for (Player o : b.players.online()) {
            if (!o.equals(gone) && Stubs.implOf(o) instanceof Players.PlayerImpl pi) Nms.echoRemove(pi, gone.getUniqueId());
        }
    }

    private static boolean chat(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Player p = player(b, f);
        String original = Wire.str(f, "message");
        String msg = original;
        if (listened(AsyncPlayerChatEvent.class)) {
            AsyncPlayerChatEvent le = new AsyncPlayerChatEvent(true, p, msg, new HashSet<>(b.players.online()));
            call(b, le);
            if (le.isCancelled()) return true;
            msg = le.getMessage();
            if (!LEGACY_FORMAT.equals(le.getFormat())) {
                out.put("rendered", Text.jsonLegacy(String.format(le.getFormat(), p.getDisplayName(), msg)));
            }
        }
        Component c = Component.text(msg);
        Component orig = Component.text(original);
        Set<Audience> viewers = new HashSet<>(b.players.online());
        viewers.add(b.console);
        AsyncChatEvent e = new AsyncChatEvent(true, p, viewers, ChatRenderer.defaultRenderer(), c, orig, SignedMessage.system(original, orig));
        call(b, e);
        if (e.isCancelled()) return true;
        if (!e.message().equals(orig)) out.put("message", Text.json(e.message()));
        if (e.renderer() != ChatRenderer.defaultRenderer()) {
            out.put("rendered", Text.json(e.renderer().render(p, p.displayName(), e.message(), b.console)));
        }
        return false;
    }

    private static boolean command(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        String msg = Wire.str(f, "message");
        PlayerCommandPreprocessEvent e = new PlayerCommandPreprocessEvent(player(b, f), msg);
        call(b, e);
        if (!msg.equals(e.getMessage())) out.put("message", e.getMessage());
        return e.isCancelled();
    }

    private static boolean move(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Location from = Players.location(b, Wire.map(f.get("from")));
        Location to = Players.location(b, Wire.map(f.get("to")));
        Location orig = to.clone();
        PlayerMoveEvent e = new PlayerMoveEvent(player(b, f), from, to);
        call(b, e);
        Location now = e.getTo();
        if (!now.equals(orig)) out.put("to", Players.locationMap(now));
        return e.isCancelled();
    }

    private static Block block(Bridge b, Map<String, Object> f, String stateKey) {
        World w = b.worlds.get(Wire.str(f, "world"));
        return Worlds.block(b, w, (int) Wire.lng(f, "x"), (int) Wire.lng(f, "y"), (int) Wire.lng(f, "z"), Wire.str(f, stateKey));
    }

    private static boolean blockBreak(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        BlockBreakEvent e = new BlockBreakEvent(block(b, f, "block"), player(b, f));
        int exp = (int) Wire.lng(f, "exp");
        e.setExpToDrop(exp);
        call(b, e);
        if (!e.isDropItems()) out.put("dropItems", false);
        if (e.getExpToDrop() != exp) out.put("expToDrop", e.getExpToDrop());
        return e.isCancelled();
    }

    private static EquipmentSlot hand(Map<String, Object> f) {
        return "OFF_HAND".equals(Wire.str(f, "hand")) ? EquipmentSlot.OFF_HAND : EquipmentSlot.HAND;
    }

    private static boolean blockPlace(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Block placed = block(b, f, "block");
        Worlds.BlockImpl impl = (Worlds.BlockImpl) Stubs.implOf(placed);
        Map<String, Object> against = new HashMap<>(Wire.map(f.get("against")));
        against.putIfAbsent("world", Wire.str(f, "world"));
        BlockPlaceEvent e = new BlockPlaceEvent(placed, Worlds.state(impl, Wire.str(f, "replaced")), block(b, against, "block"), null, player(b, f), true, hand(f));
        call(b, e);
        if (!e.canBuild()) out.put("build", false);
        return e.isCancelled();
    }

    private static boolean interact(Bridge b, Map<String, Object> f, Map<String, Object> out) {
        Action action;
        try {
            action = Action.valueOf(Wire.str(f, "action"));
        } catch (IllegalArgumentException ex) {
            action = Action.PHYSICAL;
        }
        Map<String, Object> bm = Wire.map(f.get("block"));
        Block clicked = bm.isEmpty() ? null : block(b, bm, "block");
        BlockFace face;
        try {
            face = BlockFace.valueOf(Wire.str(f, "face"));
        } catch (IllegalArgumentException ex) {
            face = BlockFace.SELF;
        }
        PlayerInteractEvent e = new PlayerInteractEvent(player(b, f), action, null, clicked, face, hand(f));
        Event.Result block0 = e.useInteractedBlock(), item0 = e.useItemInHand();
        call(b, e);
        if (e.useInteractedBlock() != block0) out.put("useInteractedBlock", e.useInteractedBlock().name());
        if (e.useItemInHand() != item0) out.put("useItemInHand", e.useItemInHand().name());
        return e.useInteractedBlock() == Event.Result.DENY && e.useItemInHand() == Event.Result.DENY;
    }
}
