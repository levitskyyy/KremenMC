package net.minecraft.world.entity.player;

import com.mojang.authlib.GameProfile;
import net.minecraft.world.entity.Entity;

public abstract class Player extends Entity {
    private final GameProfile gameProfile;

    protected Player(GameProfile gameProfile) { this.gameProfile = gameProfile; }

    public GameProfile getGameProfile() { return gameProfile; }
}
