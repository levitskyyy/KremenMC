package kremenmc.bridge;

import com.destroystokyo.paper.profile.PlayerProfile;
import io.papermc.paper.threadedregions.scheduler.AsyncScheduler;
import io.papermc.paper.threadedregions.scheduler.GlobalRegionScheduler;
import io.papermc.paper.threadedregions.scheduler.RegionScheduler;
import java.io.File;
import java.util.ArrayList;
import java.util.Collection;
import java.util.HashSet;
import java.util.List;
import java.util.Set;
import java.util.UUID;
import java.util.logging.Logger;
import net.kyori.adventure.audience.Audience;
import net.kyori.adventure.text.Component;
import org.bukkit.GameMode;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.UnsafeValues;
import org.bukkit.OfflinePlayer;
import org.bukkit.World;
import org.bukkit.block.Block;
import org.bukkit.command.CommandException;
import org.bukkit.command.CommandMap;
import org.bukkit.command.CommandSender;
import org.bukkit.command.ConsoleCommandSender;
import org.bukkit.command.PluginCommand;
import org.bukkit.entity.Entity;
import org.bukkit.entity.Player;
import org.bukkit.plugin.PluginDescriptionFile;
import org.bukkit.plugin.PluginManager;
import org.bukkit.plugin.ServicesManager;
import org.bukkit.plugin.messaging.Messenger;
import org.bukkit.scheduler.BukkitScheduler;

/** Methods of org.bukkit.Server that the bridge implements; the rest go through Stubs. */
public final class ServerImpl {
    private final Bridge b;

    ServerImpl(Bridge b) { this.b = b; }

    public String getName() { return b.infoString("name", "KremenMC"); }
    public String getVersion() { return b.infoString("version", "dev") + " (MC: " + getMinecraftVersion() + ")"; }
    public String getBukkitVersion() { return b.infoString("minecraftVersion", "26.3") + "-R0.1-SNAPSHOT"; }
    public String getMinecraftVersion() { return b.infoString("minecraftVersion", "26.3"); }
    public Logger getLogger() { return Logger.getLogger("Minecraft"); }
    public int getMaxPlayers() { return (int) b.infoLong("maxPlayers", 20); }
    public int getPort() { return (int) b.infoLong("port", 25565); }
    public String getIp() { return ""; }
    public boolean getOnlineMode() { return Boolean.TRUE.equals(b.info.get("onlineMode")); }
    public String getMotd() { return Text.legacy(motd()); }
    public Component motd() { Component c = Text.component(b.infoString("motd", "")); return c == null ? Component.empty() : c; }
    public boolean isPrimaryThread() { return b.main.isCurrent(); }
    public boolean isStopping() { return false; }
    public int getCurrentTick() { return (int) b.scheduler.currentTick(); }
    public double[] getTPS() { return new double[] {20, 20, 20}; }
    public GameMode getDefaultGameMode() { return GameMode.SURVIVAL; }
    public File getWorldContainer() { return new File("."); }
    public File getPluginsFolder() { return b.pluginsDir; }
    public File getUpdateFolderFile() { return new File(b.pluginsDir, "update"); }
    public String getUpdateFolder() { return "update"; }

    public Collection<? extends Player> getOnlinePlayers() { return b.players.online(); }
    public Player getPlayer(UUID id) { return b.players.get(id); }
    public Player getPlayer(String name) { return b.players.byName(name, false); }
    public Player getPlayerExact(String name) { return b.players.byName(name, true); }

    public List<Player> matchPlayer(String name) {
        List<Player> out = new ArrayList<>();
        for (Player p : b.players.online()) {
            if (p.getName().equalsIgnoreCase(name)) return List.of(p);
            if (p.getName().toLowerCase().contains(name.toLowerCase())) out.add(p);
        }
        return out;
    }

    public OfflinePlayer getOfflinePlayer(UUID id) {
        Player p = b.players.get(id);
        if (p != null) return p;
        throw new UnsupportedOperationException("kremenmc bridge: offline player data is not available");
    }

    public Player getPlayer(Entity e) { return e instanceof Player p ? p : null; }

    public PlayerProfile createProfile(UUID id) { return Players.profile(id, null); }
    public PlayerProfile createProfile(String name) { return Players.profile(null, name); }
    public PlayerProfile createProfile(UUID id, String name) { return Players.profile(id, name); }
    public PlayerProfile createProfileExact(UUID id, String name) { return Players.profile(id, name); }

    public List<World> getWorlds() { return b.worlds.all(); }
    public World getWorld(String name) { return b.worlds.get(name); }
    public World getWorld(UUID id) { return b.worlds.get(id); }

    public PluginManager getPluginManager() { return b.pluginManager; }
    public BukkitScheduler getScheduler() { return b.scheduler; }
    public ServicesManager getServicesManager() { return b.services; }
    public Messenger getMessenger() { return b.messenger; }
    public CommandMap getCommandMap() { return b.commandMap; }
    public ConsoleCommandSender getConsoleSender() { return b.console; }
    public GlobalRegionScheduler getGlobalRegionScheduler() { return b.scheduler.global; }
    public RegionScheduler getRegionScheduler() { return b.scheduler.region; }
    public AsyncScheduler getAsyncScheduler() { return b.scheduler.asyncScheduler; }

    public boolean isOwnedByCurrentRegion(Location l) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(Location l, int radius) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(World w, int cx, int cz) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(World w, int cx, int cz, int radius) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(World w, int minX, int minZ, int maxX, int maxZ) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(Block block) { return isPrimaryThread(); }
    public boolean isOwnedByCurrentRegion(Entity e) { return isPrimaryThread(); }
    public boolean isGlobalTickThread() { return isPrimaryThread(); }

    public PluginCommand getPluginCommand(String name) {
        return b.commandMap.getCommand(name) instanceof PluginCommand pc ? pc : null;
    }

    public boolean dispatchCommand(CommandSender sender, String line) throws CommandException {
        if (b.commandMap.getCommand(line.split(" ", 2)[0]) != null) {
            return b.commandMap.dispatch(sender, line);
        }
        UUID id = sender instanceof Player p ? p.getUniqueId() : null;
        return Wire.bool(b.call(Wire.OP_DISPATCH_COMMAND, id, line), 0);
    }

    public int broadcastMessage(String msg) { return send(Text.jsonLegacy(msg), ""); }
    public int broadcast(String msg, String permission) { return send(Text.jsonLegacy(msg), permission); }
    public int broadcast(Component c) { return send(Text.json(c), ""); }
    public int broadcast(Component c, String permission) { return send(Text.json(c), permission); }

    private int send(String json, String permission) {
        return (int) Wire.num(b.call(Wire.OP_BROADCAST, json, permission == null ? "" : permission), 0);
    }

    public void sendMessage(Component c) { broadcast(c); }

    public Iterable<? extends Audience> audiences() {
        List<Audience> out = new ArrayList<>(b.players.online());
        out.add(b.console);
        return out;
    }

    public Set<OfflinePlayer> getOperators() {
        Set<OfflinePlayer> out = new HashSet<>();
        for (Player p : b.players.online()) if (p.isOp()) out.add(p);
        return out;
    }

    public void shutdown() {
        b.log.warning("a plugin requested server shutdown; ignored by the bridge");
    }

    public void reload() {
        b.log.warning("reload is not supported by the kremenmc bridge");
    }

    private volatile UnsafeValues unsafe;

    public UnsafeValues getUnsafe() {
        UnsafeValues u = unsafe;
        if (u == null) unsafe = u = Stubs.proxy(UnsafeValues.class, new UnsafeImpl(b));
        return u;
    }

    @Override public String toString() { return "CraftServer{kremenmc}"; }

    /** No bytecode rewriting: plugins are loaded as-is. */
    public static final class UnsafeImpl {
        private final Bridge b;
        UnsafeImpl(Bridge b) { this.b = b; }

        public byte[] processClass(PluginDescriptionFile pdf, String path, byte[] clazz) { return clazz; }
        public void checkSupported(PluginDescriptionFile pdf) {}
        public boolean isSupportedApiVersion(String v) { return true; }
        public int getDataVersion() { return (int) b.infoLong("dataVersion", 0); }
        public int getProtocolVersion() { return (int) b.infoLong("protocol", 0); }
        public String getMainLevelName() { return "world"; }
        public Material toLegacy(Material m) { return m; }
        public Material fromLegacy(Material m) { return m; }
        public Material getMaterial(String name, int version) { return Worlds.Materials.fromState(name); }
    }
}
