package net.minecraft.network.chat.contents.objects;

import net.minecraft.world.item.component.ResolvableProfile;

public record PlayerSprite(ResolvableProfile player, boolean hat) implements ObjectInfo {}
