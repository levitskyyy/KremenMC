package net.minecraft.world.scores;

import java.util.Collection;
import java.util.Optional;
import net.minecraft.network.chat.Component;

public abstract class Team {
    public abstract String getName();
    public abstract Collection<String> getPlayers();
    public abstract boolean isAllowFriendlyFire();
    public abstract boolean canSeeFriendlyInvisibles();
    public abstract Visibility getNameTagVisibility();
    public abstract CollisionRule getCollisionRule();
    public abstract Optional<TeamColor> getColor();
    public abstract Component getPlayerPrefix();
    public abstract Component getPlayerSuffix();

    public boolean isAlliedTo(Team other) { return other == this; }

    public enum CollisionRule {
        ALWAYS("always", 0),
        NEVER("never", 1),
        PUSH_OTHER_TEAMS("pushOtherTeams", 2),
        PUSH_OWN_TEAM("pushOwnTeam", 3);

        public final String name;
        public final int id;

        CollisionRule(String name, int id) {
            this.name = name;
            this.id = id;
        }

        public static CollisionRule byName(String n) {
            for (CollisionRule r : values()) if (r.name.equals(n)) return r;
            return null;
        }
    }

    public enum Visibility {
        ALWAYS("always", 0),
        NEVER("never", 1),
        HIDE_FOR_OTHER_TEAMS("hideForOtherTeams", 2),
        HIDE_FOR_OWN_TEAM("hideForOwnTeam", 3);

        public final String name;
        public final int id;

        Visibility(String name, int id) {
            this.name = name;
            this.id = id;
        }

        public static Visibility byName(String n) {
            for (Visibility v : values()) if (v.name.equals(n)) return v;
            return null;
        }
    }
}
