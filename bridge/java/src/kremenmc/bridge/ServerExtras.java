package kremenmc.bridge;

import java.net.InetAddress;
import java.util.HashSet;
import java.util.Set;
import java.util.UUID;
import java.util.function.Consumer;
import org.bukkit.BanList;
import org.bukkit.Material;
import org.bukkit.OfflinePlayer;
import org.bukkit.block.data.BlockData;
import org.bukkit.boss.BarColor;
import org.bukkit.boss.BarFlag;
import org.bukkit.boss.BarStyle;
import org.bukkit.boss.BossBar;
import org.bukkit.scoreboard.Criteria;
import org.bukkit.scoreboard.ScoreboardManager;

/** Server methods for scoreboards, boss bars, block data and the op/ban/whitelist lists. */
abstract class ServerExtras {
    final Bridge sb;

    ServerExtras(Bridge b) { this.sb = b; }

    public ScoreboardManager getScoreboardManager() { return Scoreboards.of(sb).manager; }
    public Criteria getScoreboardCriteria(String name) { return Scoreboards.criteria(name); }
    public BossBar createBossBar(String title, BarColor color, BarStyle style, BarFlag... flags) { return BossBars.create(sb, title, color, style, flags); }

    public BlockData createBlockData(String state) { return WorldExtras.blockData(state); }
    public BlockData createBlockData(Material m) { return WorldExtras.blockData(m, null); }
    public BlockData createBlockData(Material m, String props) { return WorldExtras.blockData(m, props); }

    public BlockData createBlockData(Material m, Consumer<? super BlockData> fn) {
        BlockData d = createBlockData(m);
        if (fn != null) fn.accept(d);
        return d;
    }

    public Set<OfflinePlayer> getOperators() { return Lists.players(sb, "ops"); }
    public Set<OfflinePlayer> getWhitelistedPlayers() { return Lists.players(sb, "whitelist"); }
    public Set<OfflinePlayer> getBannedPlayers() { return Lists.players(sb, "profile"); }
    public boolean hasWhitelist() { return Lists.whitelistOn(sb); }
    public void setWhitelist(boolean v) { sb.action("whitelist_on", "value", v); }
    public boolean isWhitelistEnforced() { return hasWhitelist(); }
    public void setWhitelistEnforced(boolean v) {}
    public void reloadWhitelist() {}

    public Set<String> getIPBans() {
        Set<String> out = new HashSet<>();
        for (Object o : Lists.entries(sb, "ip")) out.add(Wire.str(Wire.map(o), "target"));
        return out;
    }

    public void banIP(String ip) { Lists.ban(sb, "ip", ip, ip, null, null, null); }
    public void unbanIP(String ip) { sb.action("pardon", "list", "ip", "target", ip); }
    public void banIP(InetAddress a) { banIP(a.getHostAddress()); }
    public void unbanIP(InetAddress a) { unbanIP(a.getHostAddress()); }

    public BanList<?> getBanList(BanList.Type t) { return Lists.banList(sb, t == BanList.Type.IP); }

    public BanList<?> getBanList(io.papermc.paper.ban.BanListType<?> t) {
        return Lists.banList(sb, t == io.papermc.paper.ban.BanListType.IP);
    }

    boolean isOp(UUID id) { return Lists.contains(sb, "ops", id); }
}
