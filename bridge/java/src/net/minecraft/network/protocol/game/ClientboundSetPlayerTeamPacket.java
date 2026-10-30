package net.minecraft.network.protocol.game;

import java.util.Collection;
import java.util.List;
import java.util.Optional;
import net.minecraft.network.chat.Component;
import net.minecraft.network.protocol.Packet;
import net.minecraft.world.scores.PlayerTeam;
import net.minecraft.world.scores.Team;
import net.minecraft.world.scores.TeamColor;

public final class ClientboundSetPlayerTeamPacket implements Packet<ClientGamePacketListener> {
    private static final int METHOD_ADD = 0, METHOD_REMOVE = 1, METHOD_CHANGE = 2, METHOD_JOIN = 3, METHOD_LEAVE = 4;

    private final int method;
    private final String name;
    private final Collection<String> players;
    private final Optional<Parameters> parameters;

    private ClientboundSetPlayerTeamPacket(String name, int method, Optional<Parameters> parameters, Collection<String> players) {
        this.name = name;
        this.method = method;
        this.parameters = parameters == null ? Optional.empty() : parameters;
        this.players = players == null ? List.of() : List.copyOf(players);
    }

    public static ClientboundSetPlayerTeamPacket createAddOrModifyPacket(PlayerTeam team, boolean add) {
        return new ClientboundSetPlayerTeamPacket(team.getName(), add ? METHOD_ADD : METHOD_CHANGE,
                Optional.of(new Parameters(team)), add ? team.getPlayers() : List.of());
    }

    public static ClientboundSetPlayerTeamPacket createRemovePacket(PlayerTeam team) {
        return new ClientboundSetPlayerTeamPacket(team.getName(), METHOD_REMOVE, Optional.empty(), List.of());
    }

    public static ClientboundSetPlayerTeamPacket createPlayerPacket(PlayerTeam team, String player, Action action) {
        return createMultiplePlayerPacket(team, List.of(player), action);
    }

    public static ClientboundSetPlayerTeamPacket createMultiplePlayerPacket(PlayerTeam team, Collection<String> players, Action action) {
        return new ClientboundSetPlayerTeamPacket(team.getName(), action == Action.ADD ? METHOD_JOIN : METHOD_LEAVE, Optional.empty(), players);
    }

    public String getName() { return name; }
    public Collection<String> getPlayers() { return players; }
    public Optional<Parameters> getParameters() { return parameters; }
    public int getMethodId() { return method; }

    public Action getTeamAction() {
        return switch (method) {
            case METHOD_ADD -> Action.ADD;
            case METHOD_REMOVE -> Action.REMOVE;
            default -> null;
        };
    }

    public Action getPlayerAction() {
        return switch (method) {
            case METHOD_ADD, METHOD_JOIN -> Action.ADD;
            case METHOD_LEAVE -> Action.REMOVE;
            default -> null;
        };
    }

    public enum Action { ADD, REMOVE }

    public static class Parameters {
        private final Component displayName;
        private final Component playerPrefix;
        private final Component playerSuffix;
        private final Team.Visibility nametagVisibility;
        private final Team.CollisionRule collisionRule;
        private final Optional<TeamColor> color;
        private final int options;

        public Parameters(PlayerTeam team) {
            displayName = team.getDisplayName();
            playerPrefix = team.getPlayerPrefix();
            playerSuffix = team.getPlayerSuffix();
            nametagVisibility = team.getNameTagVisibility();
            collisionRule = team.getCollisionRule();
            color = team.getColor();
            options = team.packOptions();
        }

        public Component getDisplayName() { return displayName; }
        public Component getPlayerPrefix() { return playerPrefix; }
        public Component getPlayerSuffix() { return playerSuffix; }
        public Team.Visibility getNametagVisibility() { return nametagVisibility; }
        public Team.CollisionRule getCollisionRule() { return collisionRule; }
        public Optional<TeamColor> getColor() { return color; }
        public int getOptions() { return options; }
    }
}
