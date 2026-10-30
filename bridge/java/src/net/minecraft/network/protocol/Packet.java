package net.minecraft.network.protocol;

import net.minecraft.network.PacketListener;

/** A packet object handed to Connection.send; the bridge translates known clientbound packets for the Go server. */
public interface Packet<T extends PacketListener> {}
