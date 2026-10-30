package net.minecraft.world.scores;

import java.util.Collection;
import java.util.LinkedHashSet;
import java.util.Optional;
import java.util.Set;
import net.minecraft.network.chat.Component;

public class PlayerTeam extends Team {
    private static final int FRIENDLY_FIRE = 1, SEE_INVISIBLES = 2;

    private final Scoreboard scoreboard;
    private final String name;
    private final Set<String> players = new LinkedHashSet<>();
    private Component displayName;
    private Component prefix = Component.empty();
    private Component suffix = Component.empty();
    private boolean allowFriendlyFire = true;
    private boolean seeFriendlyInvisibles = true;
    private Visibility nameTagVisibility = Visibility.ALWAYS;
    private Visibility deathMessageVisibility = Visibility.ALWAYS;
    private CollisionRule collisionRule = CollisionRule.ALWAYS;
    private Optional<TeamColor> color = Optional.empty();

    public PlayerTeam(Scoreboard scoreboard, String name) {
        this.scoreboard = scoreboard;
        this.name = name;
        this.displayName = Component.literal(name);
    }

    public Scoreboard getScoreboard() { return scoreboard; }
    @Override public String getName() { return name; }
    @Override public Collection<String> getPlayers() { return players; }

    public Component getDisplayName() { return displayName; }
    public void setDisplayName(Component c) { displayName = c == null ? Component.literal(name) : c; }

    @Override public Component getPlayerPrefix() { return prefix; }
    public void setPlayerPrefix(Component c) { prefix = c == null ? Component.empty() : c; }

    @Override public Component getPlayerSuffix() { return suffix; }
    public void setPlayerSuffix(Component c) { suffix = c == null ? Component.empty() : c; }

    @Override public boolean isAllowFriendlyFire() { return allowFriendlyFire; }
    public void setAllowFriendlyFire(boolean v) { allowFriendlyFire = v; }

    @Override public boolean canSeeFriendlyInvisibles() { return seeFriendlyInvisibles; }
    public void setSeeFriendlyInvisibles(boolean v) { seeFriendlyInvisibles = v; }

    @Override public Visibility getNameTagVisibility() { return nameTagVisibility; }
    public void setNameTagVisibility(Visibility v) { nameTagVisibility = v; }

    public Visibility getDeathMessageVisibility() { return deathMessageVisibility; }
    public void setDeathMessageVisibility(Visibility v) { deathMessageVisibility = v; }

    @Override public CollisionRule getCollisionRule() { return collisionRule; }
    public void setCollisionRule(CollisionRule r) { collisionRule = r; }

    @Override public Optional<TeamColor> getColor() { return color; }
    public void setColor(Optional<TeamColor> c) { color = c == null ? Optional.empty() : c; }

    public int packOptions() { return (allowFriendlyFire ? FRIENDLY_FIRE : 0) | (seeFriendlyInvisibles ? SEE_INVISIBLES : 0); }

    public void unpackOptions(int options) {
        allowFriendlyFire = (options & FRIENDLY_FIRE) != 0;
        seeFriendlyInvisibles = (options & SEE_INVISIBLES) != 0;
    }

    @Override public boolean isAlliedTo(Team other) { return other == this; }
}
