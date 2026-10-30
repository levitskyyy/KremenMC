package kremenmc.bridge;

import java.time.Duration;
import java.time.Instant;
import java.util.Date;
import net.kyori.adventure.bossbar.BossBar;
import org.bukkit.BanEntry;
import org.bukkit.Particle;
import org.bukkit.scoreboard.Scoreboard;

/** Player methods for effects, scoreboards, boss bars, particles and the op/ban/whitelist lists. */
abstract class PlayerExtras extends Effects.Living implements Particles.Spawner {
    PlayerExtras(Bridge b) { super(b); }

    abstract String playerName();

    public Scoreboard getScoreboard() { return Scoreboards.of(b).boardOf(target()); }
    public void setScoreboard(Scoreboard s) { Scoreboards.of(b).setBoard(target(), s); }

    public void showBossBar(BossBar bar) { BossBars.show(b, target(), bar); }
    public void hideBossBar(BossBar bar) { BossBars.hide(b, target(), bar); }

    @Override
    public void particle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra, Object data, boolean force) {
        Particles.spawn(b, target(), null, p, x, y, z, count, ox, oy, oz, extra, data, force);
    }

    public java.util.List<org.bukkit.entity.Entity> getNearbyEntities(double x, double y, double z) {
        return Entities.nearby(b, target(), b.players.cached(target()).getLocation(), x, y, z);
    }

    public void setOp(boolean v) { Lists.setOp(b, target(), playerName(), v); }
    public boolean isWhitelisted() { return Lists.whitelisted(b, target()); }
    public void setWhitelisted(boolean v) { Lists.setWhitelisted(b, target(), playerName(), v); }
    public boolean isBanned() { return Lists.banned(b, "profile", target().toString()); }

    public BanEntry<?> ban(String reason, Date expires, String source) { return ban(reason, expires == null ? null : expires.toInstant(), source); }
    public BanEntry<?> ban(String reason, Duration d, String source) { return ban(reason, d == null ? null : Instant.now().plus(d), source); }

    public BanEntry<?> ban(String reason, Instant expires, String source) {
        return Lists.ban(b, "profile", target().toString(), playerName(), reason, expires, source);
    }

    public BanEntry<?> ban(String reason, Date expires, String source, boolean kick) {
        BanEntry<?> e = ban(reason, expires, source);
        if (kick) b.conn.notify(Wire.OP_KICK, target(), Text.jsonLegacy(reason == null ? "Banned" : reason));
        return e;
    }

    public BanEntry<?> ban(String reason, Instant expires, String source, boolean kick) {
        BanEntry<?> e = ban(reason, expires, source);
        if (kick) b.conn.notify(Wire.OP_KICK, target(), Text.jsonLegacy(reason == null ? "Banned" : reason));
        return e;
    }

    public BanEntry<?> ban(String reason, Duration d, String source, boolean kick) {
        return ban(reason, d == null ? null : Instant.now().plus(d), source, kick);
    }
}
