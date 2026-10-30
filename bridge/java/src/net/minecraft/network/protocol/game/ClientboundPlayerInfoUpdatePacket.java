package net.minecraft.network.protocol.game;

import com.mojang.authlib.GameProfile;
import java.util.EnumSet;
import java.util.List;
import java.util.UUID;
import net.minecraft.network.chat.Component;
import net.minecraft.network.chat.RemoteChatSession;
import net.minecraft.network.protocol.Packet;
import net.minecraft.world.level.GameType;

public final class ClientboundPlayerInfoUpdatePacket implements Packet<ClientGamePacketListener> {
    private final EnumSet<Action> actions;
    private final List<Entry> entries;

    public ClientboundPlayerInfoUpdatePacket(EnumSet<Action> actions, List<Entry> entries) {
        this.actions = EnumSet.copyOf(actions);
        this.entries = List.copyOf(entries);
    }

    public ClientboundPlayerInfoUpdatePacket(EnumSet<Action> actions, Entry entry) { this(actions, List.of(entry)); }

    public ClientboundPlayerInfoUpdatePacket(Action action, Entry entry) { this(EnumSet.of(action), List.of(entry)); }

    public EnumSet<Action> actions() { return actions; }
    public List<Entry> entries() { return entries; }
    public List<Entry> newEntries() { return actions.contains(Action.ADD_PLAYER) ? entries : List.of(); }

    /** Wire order: the ordinal is the action's bit in the packet's action mask. */
    public enum Action {
        ADD_PLAYER,
        INITIALIZE_CHAT,
        UPDATE_GAME_MODE,
        UPDATE_LISTED,
        UPDATE_LATENCY,
        UPDATE_DISPLAY_NAME,
        UPDATE_LIST_ORDER,
        UPDATE_HAT
    }

    public record Entry(UUID profileId, GameProfile profile, boolean listed, int latency, GameType gameMode,
                        Component displayName, boolean showHat, int listOrder, RemoteChatSession.Data chatSession) {}
}
