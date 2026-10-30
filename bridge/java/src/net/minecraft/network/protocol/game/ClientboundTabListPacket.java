package net.minecraft.network.protocol.game;

import net.minecraft.network.chat.Component;
import net.minecraft.network.protocol.Packet;

public record ClientboundTabListPacket(Component header, Component footer) implements Packet<ClientGamePacketListener> {}
