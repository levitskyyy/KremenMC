package net.minecraft.network.protocol.game;

import net.minecraft.network.protocol.Packet;

/** objectiveName is null to reset the owner's scores in every objective. */
public record ClientboundResetScorePacket(String owner, String objectiveName) implements Packet<ClientGamePacketListener> {}
