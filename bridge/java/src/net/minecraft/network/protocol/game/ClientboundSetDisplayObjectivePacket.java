package net.minecraft.network.protocol.game;

import net.minecraft.network.protocol.Packet;
import net.minecraft.world.scores.DisplaySlot;
import net.minecraft.world.scores.Objective;

public final class ClientboundSetDisplayObjectivePacket implements Packet<ClientGamePacketListener> {
    private final DisplaySlot slot;
    private final String objectiveName;

    public ClientboundSetDisplayObjectivePacket(DisplaySlot slot, Objective objective) {
        this.slot = slot;
        this.objectiveName = objective == null ? "" : objective.getName();
    }

    public DisplaySlot getSlot() { return slot; }
    public String getObjectiveName() { return objectiveName; }
}
