package kremenmc.bridge;

import java.util.ArrayList;
import java.util.EnumSet;
import java.util.IdentityHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.UUID;
import net.kyori.adventure.bossbar.BossBar;
import net.kyori.adventure.text.Component;
import org.bukkit.boss.BarColor;
import org.bukkit.boss.BarFlag;
import org.bukkit.boss.BarStyle;
import org.bukkit.entity.Player;

/** Bukkit and Adventure boss bars, sent per viewer through the "bossbar" action. */
final class BossBars {
    private BossBars() {}

    static void send(Bridge b, UUID to, UUID bar, String op, Component title, float progress, int color, int overlay, int flags) {
        b.action("bossbar", "player", to, "id", bar, "op", op, "title", Text.json(title == null ? Component.empty() : title),
                "progress", (double) progress, "color", color, "overlay", overlay, "flags", flags);
    }

    private static final Map<BossBar, Adv> ADVENTURE = new IdentityHashMap<>();

    private static final class Adv implements BossBar.Listener {
        final Bridge b;
        final UUID id = UUID.randomUUID();
        final Set<UUID> viewers = new LinkedHashSet<>();

        Adv(Bridge b) { this.b = b; }

        static int flags(Set<BossBar.Flag> fs) {
            int f = 0;
            for (BossBar.Flag x : fs) f |= 1 << x.ordinal();
            return f;
        }

        void send(UUID to, BossBar bar, String op) {
            BossBars.send(b, to, id, op, bar.name(), bar.progress(), bar.color().ordinal(), bar.overlay().ordinal(), flags(bar.flags()));
        }

        void all(BossBar bar, String op) {
            List<UUID> vs;
            synchronized (ADVENTURE) { vs = new ArrayList<>(viewers); }
            for (UUID v : vs) send(v, bar, op);
        }

        @Override public void bossBarNameChanged(BossBar bar, Component o, Component n) { all(bar, "title"); }
        @Override public void bossBarProgressChanged(BossBar bar, float o, float n) { all(bar, "progress"); }
        @Override public void bossBarColorChanged(BossBar bar, BossBar.Color o, BossBar.Color n) { all(bar, "style"); }
        @Override public void bossBarOverlayChanged(BossBar bar, BossBar.Overlay o, BossBar.Overlay n) { all(bar, "style"); }
        @Override public void bossBarFlagsChanged(BossBar bar, Set<BossBar.Flag> added, Set<BossBar.Flag> removed) { all(bar, "flags"); }
    }

    static void show(Bridge b, UUID player, BossBar bar) {
        Adv a;
        boolean fresh;
        synchronized (ADVENTURE) {
            a = ADVENTURE.get(bar);
            if (a == null) {
                a = new Adv(b);
                ADVENTURE.put(bar, a);
                bar.addListener(a);
            }
            fresh = a.viewers.add(player);
        }
        if (fresh) a.send(player, bar, "add");
    }

    static void hide(Bridge b, UUID player, BossBar bar) {
        Adv a;
        synchronized (ADVENTURE) {
            a = ADVENTURE.get(bar);
            if (a == null || !a.viewers.remove(player)) return;
            if (a.viewers.isEmpty()) {
                ADVENTURE.remove(bar);
                bar.removeListener(a);
            }
        }
        send(b, player, a.id, "remove", null, 0, 0, 0, 0);
    }

    static void quit(UUID player) {
        synchronized (ADVENTURE) {
            for (Adv a : ADVENTURE.values()) a.viewers.remove(player);
        }
    }

    static org.bukkit.boss.BossBar create(Bridge b, String title, BarColor color, BarStyle style, BarFlag... flags) {
        return Stubs.proxy(org.bukkit.boss.BossBar.class, new BukkitBar(b, title, color, style, flags));
    }

    public static final class BukkitBar {
        final Bridge b;
        final UUID id = UUID.randomUUID();
        String title;
        BarColor color;
        BarStyle style;
        final Set<BarFlag> flags = EnumSet.noneOf(BarFlag.class);
        double progress = 1;
        boolean visible = true;
        final Set<UUID> players = new LinkedHashSet<>();

        BukkitBar(Bridge b, String title, BarColor color, BarStyle style, BarFlag... fs) {
            this.b = b;
            this.title = title == null ? "" : title;
            this.color = color == null ? BarColor.PINK : color;
            this.style = style == null ? BarStyle.SOLID : style;
            for (BarFlag f : fs) flags.add(f);
        }

        private int flagBits() {
            int f = 0;
            for (BarFlag x : flags) f |= 1 << x.ordinal();
            return f;
        }

        private void send(UUID to, String op) {
            BossBars.send(b, to, id, op, Scoreboards.leg(title), (float) progress, color.ordinal(), style.ordinal(), flagBits());
        }

        private synchronized void all(String op) {
            if (!visible) return;
            for (UUID p : players) send(p, op);
        }

        public String getTitle() { return title; }
        public void setTitle(String t) { title = t == null ? "" : t; all("title"); }
        public BarColor getColor() { return color; }
        public void setColor(BarColor c) { color = c; all("style"); }
        public BarStyle getStyle() { return style; }
        public void setStyle(BarStyle s) { style = s; all("style"); }
        public synchronized void addFlag(BarFlag f) { flags.add(f); all("flags"); }
        public synchronized void removeFlag(BarFlag f) { flags.remove(f); all("flags"); }
        public synchronized boolean hasFlag(BarFlag f) { return flags.contains(f); }
        public double getProgress() { return progress; }

        public void setProgress(double p) {
            if (p < 0 || p > 1) throw new IllegalArgumentException("Progress must be between 0.0 and 1.0 (" + p + ")");
            progress = p;
            all("progress");
        }

        public synchronized void addPlayer(Player p) {
            if (players.add(p.getUniqueId()) && visible) send(p.getUniqueId(), "add");
        }

        public synchronized void removePlayer(Player p) {
            if (players.remove(p.getUniqueId()) && visible) send(p.getUniqueId(), "remove");
        }

        public synchronized void removeAll() {
            if (visible) for (UUID p : players) send(p, "remove");
            players.clear();
        }

        public synchronized List<Player> getPlayers() {
            List<Player> out = new ArrayList<>();
            for (UUID id : players) {
                Player p = b.players.cached(id);
                if (p != null) out.add(p);
            }
            return out;
        }

        public synchronized void setVisible(boolean v) {
            if (v == visible) return;
            visible = true;
            for (UUID p : players) send(p, v ? "add" : "remove");
            visible = v;
        }

        public boolean isVisible() { return visible; }
        public void show() { setVisible(true); }
        public void hide() { setVisible(false); }
    }
}
