package kremenmc.bridge;

import java.time.Duration;
import java.time.Instant;
import java.util.Date;
import java.util.Map;
import java.util.UUID;
import org.bukkit.BanEntry;

/** OfflinePlayer methods answered from the server's op, whitelist and ban lists. */
abstract class OfflineExtras {
    private final UUID oid;

    OfflineExtras(UUID id) { this.oid = id; }

    public String getName() {
        Bridge b = Bridge.get();
        for (String l : new String[] {"ops", "whitelist"}) {
            for (Object o : Lists.entries(b, l)) {
                Map<String, Object> m = Wire.map(o);
                if (oid.equals(m.get("target"))) return Wire.str(m, "name");
            }
        }
        Map<String, Object> m = Lists.entry(b, "profile", oid.toString());
        return m == null || Wire.str(m, "name").isEmpty() ? null : Wire.str(m, "name");
    }

    public boolean isOp() { return Lists.contains(Bridge.get(), "ops", oid); }
    public void setOp(boolean v) { Lists.setOp(Bridge.get(), oid, getName(), v); }
    public boolean isWhitelisted() { return Lists.whitelisted(Bridge.get(), oid); }
    public void setWhitelisted(boolean v) { Lists.setWhitelisted(Bridge.get(), oid, getName(), v); }
    public boolean isBanned() { return Lists.banned(Bridge.get(), "profile", oid.toString()); }
    public BanEntry<?> ban(String reason, Date expires, String source) { return ban(reason, expires == null ? null : expires.toInstant(), source); }
    public BanEntry<?> ban(String reason, Duration d, String source) { return ban(reason, d == null ? null : Instant.now().plus(d), source); }
    public BanEntry<?> ban(String reason, Instant expires, String source) { return Lists.ban(Bridge.get(), "profile", oid.toString(), getName(), reason, expires, source); }
}
